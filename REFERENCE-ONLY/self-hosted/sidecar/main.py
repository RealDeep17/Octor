"""
Webtor metadata sidecar - OMDB-compatible proxy with adult DB support.

Parsing strategy borrowed from ThePornDatabase/namer:
  - Extract site, date, name from filename (namer's site.YYYY.MM.DD.name format)
  - Multi-pass TPDB search: full → skip-date → skip-name → site-only
  - Fuzzy match candidates with rapidfuzz (like namer's __evaluate_match)
  - StashDB GraphQL as secondary source
  - Falls back to real OMDB for non-adult content
"""

import json
import os
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional, List, Tuple
from urllib.parse import quote

import requests
from fastapi import FastAPI, Form, Query
from fastapi.responses import JSONResponse

try:
    from rapidfuzz import fuzz, process as rfprocess, utils as rfutils
    HAS_RAPIDFUZZ = True
except ImportError:
    HAS_RAPIDFUZZ = False

try:
    from unidecode import unidecode
except ImportError:
    def unidecode(s):
        return s

app = FastAPI()

# Simple in-memory LRU cache: title → response dict (max 500 entries)
from functools import lru_cache as _lru_cache
_RESULT_CACHE: dict = {}
_CACHE_MAX = 500

def _cache_get(key: str):
    return _RESULT_CACHE.get(key)

def _cache_set(key: str, value):
    if len(_RESULT_CACHE) >= _CACHE_MAX:
        # evict oldest
        oldest = next(iter(_RESULT_CACHE))
        del _RESULT_CACHE[oldest]
    _RESULT_CACHE[key] = value


# ── Config ────────────────────────────────────────────────────────────────────
STASHDB_API_KEY     = os.getenv("STASHDB_API_KEY")
STASHDB_ENDPOINT    = os.getenv("STASHDB_ENDPOINT", "https://stashdb.org/graphql")
THEPORNDB_API_KEY   = os.getenv("THEPORNDB_API_KEY") or os.getenv("TPDB_API_KEY")
TPDB_BASE           = os.getenv("TPDB_BASE", "https://api.theporndb.net")
NSFW_DB_ENABLED     = os.getenv("NSFW_DB_ENABLED", "true").lower() != "false"

SETTINGS_FILE = Path("/data/settings.json")

# Minimum fuzzy match score to accept a candidate (namer uses 89.9 for partial, 94.9 for full)
MATCH_THRESHOLD = 65.0

# ── Logging ───────────────────────────────────────────────────────────────────
def log(msg: str):
    sys.stderr.write(f"[sidecar] {msg}\n")
    sys.stderr.flush()

def now_iso():
    return datetime.now(timezone.utc).isoformat()

# ── Settings ──────────────────────────────────────────────────────────────────
def get_settings():
    if SETTINGS_FILE.exists():
        try:
            return json.loads(SETTINGS_FILE.read_text())
        except Exception as e:
            log(f"Error reading settings: {e}")
    return {"nsfw_db_enabled": NSFW_DB_ENABLED}

def save_settings(settings):
    SETTINGS_FILE.parent.mkdir(parents=True, exist_ok=True)
    SETTINGS_FILE.write_text(json.dumps(settings))

# ── Filename cleaning / parsing ────────────────────────────────────────────────
# namer's cleanup regexes — strip codec/quality junk before fuzzy matching
_RE_CLEANUP = [
    re.compile(r'(?i)\b(XXX|1080p|720p|2160p|4[Kk]|WEB[-. ]?DL|WEBRip|HDRip|BluRay|x264|x265|H\.?264|H\.?265|MP4|WRB|XC|SPLIT[-. ]?SCENES?|BTS|mkv|mp4|avi|wmv|mov)\b'),
    re.compile(r'(?i)\b(PROPER|REPACK|READNFO|INTERNAL|LIMITED)\b'),
    re.compile(r'\[.*?\]'),
    re.compile(r'\(.*?\)'),
]

def name_cleaner(name: str) -> str:
    for rx in _RE_CLEANUP:
        name = rx.sub('', name)
    name = re.sub(r'[-_.]+', ' ', name)
    name = re.sub(r'\s+', ' ', name).strip('- ')
    return name

# namer's canonical filename format: Site.YYYY.MM.DD.Scene.Title.ext
# Also handles: Site - YYYY-MM-DD - Title
_DATE_RE = re.compile(
    r'(?P<year>(?:19|20)\d{2})[.\- ]+(?P<month>\d{2})[.\- ]+(?P<day>\d{2})'
)

def parse_adult_filename(title: str) -> dict:
    """
    Parse an adult torrent name into {site, date, name}.
    Mirrors namer's parse_file_name logic.
    """
    # strip extension
    stem = re.sub(r'\.(mp4|mkv|avi|mov|wmv|webm|ts)$', '', title, flags=re.I)
    # normalise separators to spaces first for site detection
    normalised = re.sub(r'[._]+', ' ', stem)

    result = {"site": None, "date": None, "name": None, "raw": title}

    m = _DATE_RE.search(normalised)
    if m:
        year = m.group("year")
        month = m.group("month")
        day = m.group("day")
        result["date"] = f"{year}-{month}-{day}"

        # site = everything before the date
        before = normalised[:m.start()].strip(' -')
        result["site"] = before if before else None

        # name = everything after the date
        after = normalised[m.end():].strip(' -')
        result["name"] = name_cleaner(after) if after else None
    else:
        # No date — treat whole thing as scene name
        result["name"] = name_cleaner(normalised)

    log(f"Parsed filename → site={result['site']!r} date={result['date']!r} name={result['name']!r}")
    return result

# ── NSFW studio detection ─────────────────────────────────────────────────────
# Normalised (lowercase, no separators) studio names that unambiguously
# indicate adult content when found in a torrent title.
# Rule: only include names that CANNOT appear in mainstream movie/show titles.
_NSFW_STUDIOS = {
    # ── Vixen Media Group ──────────────────────────────────────────────────────
    "vixenmedia", "vixen",
    "blacked", "blackedraw",
    "tushy", "tushyraw",
    "deeper",
    "slayed",

    # ── MindGeek / Brazzers family ────────────────────────────────────────────
    "brazzers", "brazzersexxtra", "brazzersvip", "brazzersnetwork",
    "realitykings",
    "mofos", "mofosnetwork",
    "digitalplayground",
    "babes",
    "fakehub",

    # ── Bang Bros ─────────────────────────────────────────────────────────────
    "bangbros", "bangbrosnetwork", "bangreal",

    # ── Team Skeet family ─────────────────────────────────────────────────────
    "teamskeet", "skeetclub",
    "familystrokes",
    "stepsiblingscaught",
    "tiny4k",
    "exploitedcollegegirls", "exploitedteens",
    "trickymasseur",
    "nubilesporn", "nubiles",

    # ── Adult Time family ─────────────────────────────────────────────────────
    "adulttime",
    "girlsway",
    "sweetheartvideo",
    "lesbea",
    "puretaboo",
    "transangels", "tsseduction",
    "bellesa", "bellesafilms",
    "wankitnow",

    # ── Naughty America ───────────────────────────────────────────────────────
    "naughtyamerica",

    # ── Evil Angel / Kink ────────────────────────────────────────────────────
    "evilangel",
    "kinkdotcom", "kink",
    "deviantshardcore",

    # ── Jules Jordan / Elegant Angel ─────────────────────────────────────────
    "julesjordan",
    "elegantangel",

    # ── Girlfriends Films / Mile High ────────────────────────────────────────
    "girlfriendsfilms",
    "milehighpictures",
    "metrohd",

    # ── New Sensations / Reality Junkies ─────────────────────────────────────
    "newsensations",
    "realityjunkies",
    "pornfidelity", "pornfid",
    "teenfidelity",
    "maturesfidelity",
    "realityjunkiesoriginalseries",

    # ── WOW Girls / Let's Do It / Dorcel ────────────────────────────────────
    "wowgirls",
    "letsdoeit",
    "dorcelclub", "marcdorcel",

    # ── Twistys / Passion HD / PassionHD ────────────────────────────────────
    "twistys",
    "passionhd",
    "rickysroom",

    # ── Gonzo / taboo ─────────────────────────────────────────────────────────
    "tabooheat", "famsex",
    "momsbangteens", "momsteachsex", "mommysgirl",
    "dogfartnetwork",
    "ghettogaggers",
    "blacksonblondes",
    "interracialpass",

    # ── Art / Solo ────────────────────────────────────────────────────────────
    "metart", "metartvip",
    "femjoy",
    "hegre",
    "sexart",
    "thelifeerotic",
    "wearehairy",
    "abbywinterscom", "abbywinters",

    # ── European ──────────────────────────────────────────────────────────────
    "21sextury", "21naturals", "21footart",
    "fakings",
    "clubseventeen",
    "eroticax",
    "penthouse",
    "wicked",
    "vivid",

    # ── Other notable studios ─────────────────────────────────────────────────
    "naughtyamerica",
    "teens3some",
    "wankitnow",
    "pervcity",
    "devilsfilm",
    "spizoo",
    "sweetheart",
    "holed",                # Vixen sub-brand
    "eroticbpm",
    "scoreland",
    "scorevideo",
    "bigtitsatwork", "bigtitsboss",
    "momxxx",
    "fakehospital", "faketaxi", "fakeagent",  # FakeHub brands
    "mofosexclusive",
    "erito",
    "caribbeancom", "caribbean",

    # ── JAV/Asian studio names in filenames ───────────────────────────────────
    # (JAV codes are handled separately; these cover name-based tags)
    "1pondo", "1pon",
    "heyzo",
    "mgstage",
    "10musume",
    "pacopacomama",
    "muramura",
    "tokyohot",
    "javhd",
    "jpanesemom",
}

def studio_in_title(title: str) -> bool:
    """Return True only if a known adult studio name appears in the title."""
    t_no_sep = re.sub(r'[^a-z0-9]', '', title.lower())
    return any(s in t_no_sep for s in _NSFW_STUDIOS)

def looks_nsfw(title: str) -> bool:
    """
    Detect adult content by three signals (JAV handled separately):

    1. Known studio name in title (explicit list)
    2. Explicit XXX token
    3. STRUCTURAL: adult torrent pattern Site.YYYY.MM.DD.Name
       Adult releases put the date right after the studio name.
       Mainstream movies put the year at the END: Movie.Title.2024.BluRay
       So if we see: <word(s)>.<YYYY.MM.DD>.<more-words>
       with NO quality/codec tag before the date, it's almost certainly adult.
    """
    if studio_in_title(title):
        return True

    # Explicit XXX as a standalone token
    if re.search(r'(?:^|[\s._\-\[\(])xxx(?:[\s._\-\]\)]|$)', title, re.I):
        return True

    # Structural heuristic: Site.YYYY.MM.DD.SceneTitle
    # Require: something before the date (potential studio name), and something
    # after (scene title). The "something before" must NOT be a year alone
    # (that's a mainstream movie like "Film.2024.BluRay").
    # Also reject if a codec/quality tag appears BEFORE the date — that's
    # mainstream: "Movie.Title.BluRay.2024.x264" has BluRay before the date.
    _CODEC_TAGS = re.compile(
        r'(?i)\b(bluray|blu-ray|webrip|web-dl|webdl|hdtv|dvdrip|bdrip|'
        r'hdrip|camrip|1080p|720p|2160p|480p|4k|hevc|x264|x265|xvid|'
        r'avc|remux|repack|proper)\b'
    )
    stem = re.sub(r'\.(mp4|mkv|avi|mov|wmv|webm|ts)$', '', title, flags=re.I)
    m = _DATE_RE.search(re.sub(r'[._]+', ' ', stem))
    if m:
        before = re.sub(r'[._]+', ' ', stem[:m.start()]).strip()
        after  = re.sub(r'[._]+', ' ', stem[m.end():]).strip()
        # Must have a studio-like prefix (1+ words, not just a bare year)
        # and must have scene title after the date
        if (before
                and after
                and not re.match(r'^(19|20)\d{2}$', before.split()[-1])
                and not _CODEC_TAGS.search(before)):
            return True

    return False


# ── JAV detection & lookup ────────────────────────────────────────────────────
# JAV codes follow the pattern: LETTERS-DIGITS  e.g. SSIS-756, IPX-576, HMN-123
# We require the prefix to be 2-6 letters and the number to be 2-5 digits.
# To avoid false matches on non-JAV codes (e.g. "HD-720", "MP4-1080"),
# we also maintain a set of known-NOT-JAV prefixes to reject.
_NOT_JAV_PREFIXES = {
    "hd", "mp4", "mkv", "avi", "ts", "web", "dl", "blu", "ray",
    "uhd", "hevc", "avc", "hdr", "sdr", "dvd", "bd",
    "ep", "s0", "s1", "s2", "s3",   # season/episode tags
}

_JAV_CODE_RE = re.compile(
    r'(?:^|[\s._\-\[\(])([A-Z]{2,6})[-_ ]?(\d{2,5})(?:[\s._\-\]\)]|$)',
    re.IGNORECASE,
)
# Date-based JAV sites: site-YYMMDD-NNN
_JAV_DATE_RE = re.compile(
    r'(?i)(?:^|[\s._\-])?(1pondo|caribbeancom|caribbean|10musume|heyzo|pacopacomama)[-_.]?(\d{6})[-_](\d{2,3})',
)

def extract_jav_code(title: str) -> Optional[str]:
    """Extract a JAV code from a title. Returns normalised 'XXXX-NNN' or None."""
    stem = re.sub(r'\.(mp4|mkv|avi|mov|wmv|webm|ts)$', '', title, flags=re.I)

    # Date-based sites first
    m = _JAV_DATE_RE.match(stem)
    if m:
        return f"{m.group(1)}-{m.group(2)}_{m.group(3)}"

    # Standard letter-number code
    m = _JAV_CODE_RE.search(stem)
    if m:
        prefix = m.group(1).upper()
        number = m.group(2)
        if prefix.lower() in _NOT_JAV_PREFIXES:
            return None
        # Reject if the whole stem is just the "code" and it looks like a season tag
        if re.match(r'^S\d{1,2}$', prefix, re.I):
            return None
        return f"{prefix}-{number.zfill(3)}"
    return None

def is_jav(title: str) -> bool:
    return extract_jav_code(title) is not None

def tpdb_jav_lookup(code: str) -> Optional[dict]:
    """Look up a JAV code via TPDB's /jav endpoint."""
    if not THEPORNDB_API_KEY:
        return None
    url = f"{TPDB_BASE}/jav?parse={quote(code)}&limit=5"
    log(f"TPDB JAV search: {url}")
    try:
        r = requests.get(url, headers=_TPDB_HEADERS(), timeout=5)
        r.raise_for_status()
        data = r.json().get("data") or []
        if not data:
            return None
        # Pick the scene whose external_id/title best matches our code
        code_clean = re.sub(r'[^a-z0-9]', '', code.lower())
        for scene in data:
            ext_id = re.sub(r'[^a-z0-9]', '', (scene.get("external_id") or "").lower())
            if ext_id and (ext_id == code_clean or code_clean in ext_id or ext_id in code_clean):
                return _normalise_tpdb_jav(scene)
        # fallback: first result
        return _normalise_tpdb_jav(data[0])
    except Exception as e:
        log(f"TPDB JAV lookup failed: {e}")
        return None

def _normalise_tpdb_jav(d: dict) -> dict:
    """Flatten TPDB JAV response."""
    site_obj = d.get("site") or {}
    poster = d.get("poster")
    if not poster and isinstance(d.get("posters"), dict):
        poster = d["posters"].get("full") or d["posters"].get("large")
    if not poster:
        poster = d.get("image")
    return {
        "id": d.get("_id") or d.get("id"),
        "title": d.get("title"),
        "date": d.get("date"),
        "site": site_obj.get("name") if isinstance(site_obj, dict) else None,
        "description": d.get("description") or "",
        "performers": d.get("performers") or [],
        "tags": [t.get("name") for t in d.get("tags") or [] if t.get("name")],
        "poster": poster,
        "duration": d.get("duration"),
        "rating": d.get("rating"),
        "url": d.get("url"),
        "_source": "tpdb_jav",
    }


# ── Fuzzy matching (namer-style) ───────────────────────────────────────────────
def fuzzy_score(query: Optional[str], candidate: str) -> float:
    if not query or not candidate:
        return 0.0
    if HAS_RAPIDFUZZ:
        return rfutils.default_process(query) and fuzz.WRatio(
            rfutils.default_process(query),
            rfutils.default_process(candidate),
        ) or 0.0
    # fallback: simple token overlap
    q_tokens = set(query.lower().split())
    c_tokens = set(candidate.lower().split())
    if not q_tokens:
        return 0.0
    overlap = len(q_tokens & c_tokens)
    return overlap / max(len(q_tokens), len(c_tokens)) * 100

def best_candidate_score(query: Optional[str], candidates: List[str]) -> float:
    """Return highest fuzzy score between query and any candidate."""
    if not query or not candidates:
        return 0.0
    best = 0.0
    for c in candidates:
        s = fuzzy_score(query, c)
        if s > best:
            best = s
    return best

def score_result(parsed: dict, scene: dict) -> float:
    """
    Score a TPDB scene against parsed filename parts.
    Mirrors namer's __match_weight logic:
      site match  → +100
      date match  → +100
      name fuzzy  → 0-100
    Best possible = 300.
    """
    score = 0.0

    # Site match
    if parsed.get("site") and scene.get("site"):
        parsed_site = re.sub(r'[^a-z0-9]', '', unidecode(parsed["site"]).lower())
        scene_site  = re.sub(r'[^a-z0-9]', '', unidecode(scene["site"]).lower())
        if parsed_site and (parsed_site in scene_site or scene_site in parsed_site):
            score += 100
    elif not parsed.get("site"):
        # No site info in filename — don't penalise
        score += 50

    # Date match
    if parsed.get("date") and scene.get("date"):
        if parsed["date"] == scene["date"][:10]:
            score += 100
    elif not parsed.get("date"):
        score += 50  # no date info — don't penalise

    # Name fuzzy match against scene title + performer names
    if parsed.get("name"):
        candidates = []
        if scene.get("title"):
            candidates.append(scene["title"])
        for p in scene.get("performers") or []:
            name = p.get("name") or (p.get("performer") or {}).get("name")
            if name:
                candidates.append(name)

        name_score = best_candidate_score(parsed["name"], candidates)
        score += name_score

    return score

# ── TPDB REST API ─────────────────────────────────────────────────────────────
_TPDB_HEADERS = lambda: {
    "Authorization": f"Bearer {THEPORNDB_API_KEY}",
    "Accept": "application/json",
    "User-Agent": "webtor-sidecar/2",
}

def tpdb_search(site: Optional[str], date: Optional[str], name: Optional[str], limit: int = 10) -> List[dict]:
    """
    Multi-param TPDB search using their `parse` query string (same as namer's __build_url).
    Format: /scenes?parse=site.YYYY-MM-DD.name&limit=N
    """
    if not THEPORNDB_API_KEY:
        return []

    # Build parse string (namer's format)
    parts = []
    if site:
        parts.append(re.sub(r'[^a-z0-9]', '', unidecode(site).lower()))
    if date:
        parts.append(date)
    if name:
        parts.append(name)

    parse_str = '.'.join(parts) if parts else None
    if not parse_str:
        return []

    url = f"{TPDB_BASE}/scenes?parse={quote(parse_str)}&limit={limit}"
    log(f"TPDB search: {url}")
    try:
        r = requests.get(url, headers=_TPDB_HEADERS(), timeout=5)
        r.raise_for_status()
        data = r.json().get("data") or []
        # Normalise structure
        return [_normalise_tpdb(d) for d in data]
    except Exception as e:
        log(f"TPDB search failed: {e}")
        return []

def tpdb_search_raw_q(q: str, limit: int = 10) -> List[dict]:
    """Plain text search fallback."""
    if not THEPORNDB_API_KEY:
        return []
    url = f"{TPDB_BASE}/scenes?q={quote(q)}&per_page={limit}"
    log(f"TPDB raw search: {url}")
    try:
        r = requests.get(url, headers=_TPDB_HEADERS(), timeout=5)
        r.raise_for_status()
        data = r.json().get("data") or []
        return [_normalise_tpdb(d) for d in data]
    except Exception as e:
        log(f"TPDB raw search failed: {e}")
        return []

def _normalise_tpdb(d: dict) -> dict:
    """Flatten TPDB scene response to a common dict."""
    site_obj = d.get("site") or {}
    return {
        "id": d.get("_id") or d.get("id") or d.get("slug"),
        "title": d.get("title"),
        "date": d.get("date") or d.get("release_date") or d.get("production_date"),
        "site": site_obj.get("name") if isinstance(site_obj, dict) else None,
        "description": d.get("description") or d.get("details"),
        "performers": d.get("performers") or [],
        "tags": [t.get("name") for t in d.get("tags") or [] if t.get("name")],
        "poster": d.get("poster") or d.get("poster_image") or d.get("image"),
        "duration": d.get("duration"),
        "rating": d.get("rating"),
        "url": d.get("url"),
        "_source": "tpdb",
    }

# ── StashDB GraphQL ───────────────────────────────────────────────────────────
_STASHDB_QUERY = """
query ($term: String!) {
  searchScene(term: $term) {
    id title details release_date duration
    studio { name }
    performers { performer { name } as }
    tags { name }
    images { url width height }
    urls { url site { name } }
  }
}
"""

def stashdb_search(term: str) -> List[dict]:
    if not STASHDB_API_KEY or not term:
        return []
    headers = {"Content-Type": "application/json", "Apikey": STASHDB_API_KEY}
    try:
        r = requests.post(
            STASHDB_ENDPOINT,
            headers=headers,
            json={"query": _STASHDB_QUERY, "variables": {"term": term}},
            timeout=5,
        )
        r.raise_for_status()
        data = r.json()
        if data.get("errors"):
            log(f"StashDB errors: {data['errors']}")
            return []
        scenes = (data.get("data") or {}).get("searchScene") or []
        return [_normalise_stashdb(s) for s in scenes]
    except Exception as e:
        log(f"StashDB search failed: {e}")
        return []

def _normalise_stashdb(s: dict) -> dict:
    images = s.get("images") or []
    poster = images[0].get("url") if images else None
    studio = s.get("studio") or {}
    return {
        "id": s.get("id"),
        "title": s.get("title"),
        "date": s.get("release_date"),
        "site": studio.get("name") if isinstance(studio, dict) else None,
        "description": s.get("details"),
        "performers": s.get("performers") or [],
        "tags": [t.get("name") for t in s.get("tags") or [] if t.get("name")],
        "poster": poster,
        "duration": s.get("duration"),
        "rating": None,
        "url": None,
        "_source": "stashdb",
    }

# ── Multi-strategy NSFW lookup (namer-style passes) ───────────────────────────
def nsfw_lookup(title: str) -> Optional[dict]:
    """
    Namer-style multi-pass search:
      JAV fast-path: if title looks like a JAV code → TPDB /jav endpoint
      Pass 1: site + date + name
      Pass 2: site + name (skip date)
      Pass 3: site + date (skip name)
      Pass 4: name only
      Pass 5: raw text search
      Pass 6: StashDB (parallel source)
    Each pass collects candidates → score all → pick best above threshold.
    """
    # ── JAV fast-path ──────────────────────────────────────────────────────────
    jav_code = extract_jav_code(title)
    if jav_code:
        log(f"Detected JAV code: {jav_code}")
        result = tpdb_jav_lookup(jav_code)
        if result:
            return _to_omdb(result)
        log(f"JAV lookup missed for {jav_code}, falling through to scene search")

    parsed = parse_adult_filename(title)
    site  = parsed.get("site")
    date  = parsed.get("date")
    name  = parsed.get("name")

    # Build search passes (mirrors namer's __metadata_api_lookup_type)
    passes: List[Tuple[Optional[str], Optional[str], Optional[str]]] = []
    if site or date or name:
        passes.append((site, date, name))           # Pass 1: full
    if date:
        passes.append((site, None, name))           # Pass 2: skip date
    if name:
        passes.append((site, date, None))           # Pass 3: skip name
    if site:
        passes.append((None, date, name))           # Pass 4: skip site
    passes.append((None, None, name or title))      # Pass 5: name only

    all_candidates: List[dict] = []
    seen_ids: set = set()

    for s, d, n in passes:
        results = tpdb_search(s, d, n)
        for r in results:
            rid = r.get("id")
            if rid and rid not in seen_ids:
                seen_ids.add(rid)
                all_candidates.append(r)
        # If we already have a high-confidence match, stop early
        if all_candidates:
            best = _pick_best(parsed, all_candidates)
            if best and best[1] >= 200:  # site+date both matched
                log(f"Early stop: high confidence match '{best[0].get('title')}' score={best[1]:.1f}")
                return _to_omdb(best[0])

    # Pass 6: raw text fallback via TPDB
    if not all_candidates:
        clean = name_cleaner(title)
        raw_results = tpdb_search_raw_q(clean)
        for r in raw_results:
            rid = r.get("id")
            if rid and rid not in seen_ids:
                seen_ids.add(rid)
                all_candidates.append(r)

    # Pass 7: StashDB
    stash_term = name or name_cleaner(title)
    stash_results = stashdb_search(stash_term)
    for r in stash_results:
        rid = r.get("id")
        if rid and rid not in seen_ids:
            seen_ids.add(rid)
            all_candidates.append(r)

    if not all_candidates:
        log(f"No candidates found for '{title}'")
        return None

    best = _pick_best(parsed, all_candidates)
    if not best:
        return None

    scene, score = best
    log(f"Best match: '{scene.get('title')}' score={score:.1f} source={scene.get('_source')}")

    if score < MATCH_THRESHOLD:
        log(f"Score {score:.1f} below threshold {MATCH_THRESHOLD} — rejected")
        return None

    return _to_omdb(scene)

def _pick_best(parsed: dict, candidates: List[dict]) -> Optional[Tuple[dict, float]]:
    if not candidates:
        return None
    scored = [(c, score_result(parsed, c)) for c in candidates]
    scored.sort(key=lambda x: x[1], reverse=True)
    return scored[0]

# ── Convert scene to OMDB-compatible response ─────────────────────────────────
def _year_from_date(value: Any) -> str:
    if not value:
        return "N/A"
    m = re.search(r'(19|20)\d{2}', str(value))
    return m.group(0) if m else "N/A"

def _format_runtime(value: Any) -> str:
    if value in (None, ""):
        return "N/A"
    try:
        seconds = int(float(value))
    except (TypeError, ValueError):
        return str(value)
    minutes = max(1, round(seconds / 60))
    return f"{minutes} min"

def _performer_names(performers: List[dict]) -> List[str]:
    names = []
    for p in performers:
        # TPDB: {"name": "X"} or {"performer": {"name": "X"}}
        name = p.get("name") or (p.get("performer") or {}).get("name")
        if name:
            names.append(name)
    return names

def _to_omdb(scene: dict) -> dict:
    performers = _performer_names(scene.get("performers") or [])
    tags = scene.get("tags") or []
    source = scene.get("_source", "tpdb")
    scene_id = scene.get("id") or scene.get("title")
    return {
        "Title":      scene.get("title") or "N/A",
        "Year":       _year_from_date(scene.get("date")),
        "Rated":      "XXX",
        "Released":   scene.get("date") or "N/A",
        "Runtime":    _format_runtime(scene.get("duration")),
        "Genre":      ", ".join(tags[:8]) if tags else "Adult",
        "Director":   "N/A",
        "Actors":     ", ".join(performers[:8]) if performers else "N/A",
        "Plot":       scene.get("description") or "N/A",
        "Language":   "N/A",
        "Country":    "N/A",
        "Awards":     "N/A",
        "Poster":     scene.get("poster") or "N/A",
        "Ratings":    [],
        "Metascore":  "N/A",
        "imdbRating": str(scene.get("rating") or "N/A"),
        "imdbVotes":  "N/A",
        "imdbID":     f"{source}:{scene_id}",
        "Type":       "movie",
        "DVD":        "N/A",
        "BoxOffice":  "N/A",
        "Production": scene.get("site") or "N/A",
        "Website":    scene.get("url") or "N/A",
        "Response":   "True",
    }

# ── FastAPI endpoints ─────────────────────────────────────────────────────────
#
# NOTE: This sidecar sits in the OMDB slot of the enricher chain:
#   TMDB (primary) → OMDB=this sidecar (secondary) → Kinopoisk (tertiary)
#
# TMDB already handles all mainstream movies upstream, so by the time we are
# called, the content is either adult (TMDB returns nothing) or an obscure
# title TMDB missed (very rare).  We focus exclusively on adult DBs here.
# We do NOT relay to the real omdbapi.com — that would be redundant.
#
@app.get("/")
def metadata_proxy(
    t: Optional[str] = Query(None),
    i: Optional[str] = Query(None),
    apikey: Optional[str] = Query(None),
    plot: Optional[str] = Query("short"),
):
    if not t and not i:
        return JSONResponse({"Response": "False", "Error": "No title or ID"}, status_code=400)

    settings = get_settings()
    nsfw_enabled = settings.get("nsfw_db_enabled", True)

    # ── Cache check ─────────────────────────────────────────────────────────────
    cache_key = f"{t}|{i}"
    cached = _cache_get(cache_key)
    if cached is not None:
        log(f"Cache hit for {cache_key!r}")
        return cached

    # ── Path 1: JAV code ────────────────────────────────────────────────────────
    jav_code = extract_jav_code(t) if t else None
    if jav_code and nsfw_enabled:
        log(f"JAV fast-path for code: {jav_code}")
        result = tpdb_jav_lookup(jav_code)
        if result:
            resp = _to_omdb(result)
            _cache_set(cache_key, resp)
            return resp
        return JSONResponse({"Response": "False", "Error": "JAV not found"}, status_code=404)

    # ── Path 2: Western adult scene ─────────────────────────────────────────────
    # Only search when a known studio name OR structural date pattern is present.
    if t and nsfw_enabled and looks_nsfw(t):
        log(f"Adult scene search for: {t!r}")
        data = nsfw_lookup(t)
        if data:
            _cache_set(cache_key, data)
            return data

    # ── Everything else: 404 ────────────────────────────────────────────────────
    # TMDB (upstream) and Kinopoisk (downstream) handle mainstream content.
    return JSONResponse({"Response": "False", "Error": "Movie not found!"}, status_code=404)


@app.get("/settings")
def view_settings():
    return get_settings()


@app.post("/settings")
def update_settings(nsfw_enabled: bool = Form(...)):
    settings = {"nsfw_db_enabled": nsfw_enabled}
    try:
        save_settings(settings)
    except Exception as e:
        log(f"Error saving settings: {e}")
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)
    return {"status": "ok", "nsfw_db_enabled": nsfw_enabled}
