"""
Octor metadata sidecar - OMDB-compatible proxy with adult DB support.

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
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional, List, Tuple
from urllib.parse import quote

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry
from fastapi import FastAPI, Form, Query
from fastapi.responses import JSONResponse

# Global HTTP session for connection pooling (dramatically reduces handshake latency)
SESSION = requests.Session()
_RETRY_STRATEGY = Retry(
    total=3,
    backoff_factor=1,
    status_forcelist=[429, 500, 502, 503, 504],
)
SESSION.mount("https://", HTTPAdapter(max_retries=_RETRY_STRATEGY, pool_connections=32, pool_maxsize=32))
SESSION.mount("http://", HTTPAdapter(max_retries=_RETRY_STRATEGY, pool_connections=32, pool_maxsize=32))

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
SIDECAR_ENRICHMENT_ENABLED = os.getenv("SIDECAR_ENRICHMENT_ENABLED", "true").lower() != "false"

SETTINGS_FILE = Path(os.getenv("SIDECAR_DATA_DIR", "./data") + "/settings.json")

# Composite match score threshold — NOT a raw fuzzy string score.
# Positive signals: site match (+80–120), performer (+150), date exact (+100), name fuzzy (+0–100),
# no-site/date baselines (+50 each). A score of 100 requires at least one strong signal.
MATCH_THRESHOLD = 100.0

# Global thread pool for parallel search tasks (reuses threads and caps outgoing connections)
SEARCH_EXECUTOR = ThreadPoolExecutor(max_workers=32)

@app.on_event("shutdown")
def shutdown_event():
    log("Shutting down search executor pool")
    SEARCH_EXECUTOR.shutdown(wait=False)

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
    return {"sidecar_enrichment_enabled": SIDECAR_ENRICHMENT_ENABLED}

def save_settings(settings):
    SETTINGS_FILE.parent.mkdir(parents=True, exist_ok=True)
    SETTINGS_FILE.write_text(json.dumps(settings))

# ── Filename cleaning / parsing ────────────────────────────────────────────────
# namer's cleanup regexes — strip codec/quality junk before fuzzy matching
_RE_CLEANUP = [
    re.compile(r'(?i)\b(XXX|1080p|720p|2160p|4[Kk]|WEB[-. ]?DL|WEBRip|HDRip|BluRay|x264|x265|H\.?264|H\.?265|MP4|WRB|XC|SPLIT[-. ]?SCENES?|BTS|mkv|mp4|avi|wmv|mov|rq)\b'),
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

# ── NSFW studio detection ─────────────────────────────────────────────────────
# Normalised (lowercase, no separators) studio names that unambiguously
# indicate adult content when found in a torrent title.
# Curated minimal baseline of the most popular studios (all others are resolved dynamically).
_NSFW_STUDIOS = {
    "10musume", "18eighteen", "18vr", "1pondo", "2chickssametime", "2girlsonecup", "40somethingmag", "50plusmilfs",
    "5kporn", "60plusmilfs", "8kmilfs", "8thstreetlatinas", "adameve", "adamevepictures", "adulttime", "allfinegirls",
    "allherluv", "alsscan", "americandaydreams", "amourbabes", "analintroductions", "analmom", "analtherapyxxx", "archiveofstevehicks",
    "assparade", "avstars", "babes", "babygotboobs", "backroomcastingcouch", "badoinkvr", "hotmilfsfuck", "bamvisions", "bangbros",
    "bangbus", "bangcasting", "bangingbeauties", "bangme", "bangpov", "bbwchan", "beautyandsenior", "bellesablinddate",
    "bellesafilms", "bffs", "bifuckkink", "bigbuttslikeitbig", "bigmouthfuls", "bignaturals", "bigtitsatschool", "bigtitsatwork",
    "bigtitsboss", "bigtitsinsports", "bigtitsinuniform", "bigtitsroundasses", "bigwetbutts", "bikebabes", "bikinicrashers", "bizarrevideotranssexual",
    "blackandstacked", "blacked", "blackedraw", "blackisbetter", "blackmailed", "blackmeatwhitetreat", "blackonblondes", "blacksonblondes",
    "blacksoncougars", "blacksonsluts", "blackstepdad", "blackvalleygirls", "blackwhitefuckfest", "bleufilms", "bloodangels", "blowjobfridays",
    "blowjobninjas", "bluecollarbabes", "bluefantasies", "bodyinmind", "bondagecompound", "bonedathome", "bonusholeboys", "boobsquad",
    "bootyfullbabes", "bootyliciousmag", "boppingbabes", "borderpatrolsex", "bossymilfs", "boundgagged", "boundgangbangs", "boundgods",
    "boundinpublic", "bountyhunterporn", "bracefaced", "brattamer", "brattybabesownyou", "brattybarebabes", "brattymilf", "brattysis",
    "brazzers", "brazzersexxtra", "breedingmaterial", "britishbukkakebabes", "brokenbabes", "brownbunnies", "bubblegumdungeon", "burningangel",
    "busstop", "bustedbabysitters", "bustyadventures", "bustyangelique", "bustyarianna", "bustycollegecoeds", "bustydanniashe", "bustydustystash",
    "bustyinescudna", "bustykellykay", "bustykerrymarie", "bustylornamorgan", "bustymerilyn", "bustyoldsluts", "bustyones", "bustysammieblack",
    "buttdivers", "buttmachineboys", "buttman", "canhescore", "captivemale", "cardiogasm", "caribbeancom", "caribbeancompr",
    "caughtfapping", "caughtmycoach", "cfnm", "cfnmshow", "cfnmteens", "chasewaterbabes", "chastitybabes", "cheatinghotbabes",
    "cheatingsis", "cheatingwithmyex", "cherrybrady", "cherrypop", "chongas", "ciaobella", "cinematickink", "clinicaltorments",
    "clubsweethearts", "cock4stepmom", "collegebabesexposed", "collegebash", "collegerules", "colombiafuckfest", "cosplaybabes", "cosplayground",
    "cougarseductions", "coupleswapping", "creampieforgranny", "creampiefunbabes", "cruelmedia", "cuckoldsessions", "cum4k", "cumfiesta",
    "cumswappingsis", "cutiepie", "czechanalsx", "czechboobs", "czechcasting", "czechfantasy", "czechharem", "czechhunter",
    "czechmassage", "czechporncastle", "czechpublic", "czechstreets", "czechsuperstars", "czechvr", "czechvrnetwork", "czechwives",
    "dadcrush", "daddygetslucky", "daddypounds", "daddysboy", "daddysgirl", "daddyslilangel", "dadsloveporn", "daughterswap",
    "ddfbabes", "ddfbusty", "ddfnetwork", "ddfprod", "deeper", "detentiongirls", "devicebondage", "devilsfilm",
    "diaryofamilf", "diaryofananny", "digitalplayground", "digitalsin", "dilfed", "familyswap", "dirtcheapteens", "dirtymasseur", "dirtyworldtour",
    "divinebitches", "doctoradventures", "dogfartnetwork", "doghousedigital", "domai", "dontbreakme", "dorcel", "dorcelaoc",
    "dorminvasion", "driverxxx", "dronehunter", "dungeonsex", "dyked", "ebonythots", "edgedandbound", "electrosluts",
    "elegantanal", "elegantangel", "enjoyx", "eroticababes", "eroticax", "eroticbeauty", "erotique", "eternaldesire",
    "eurobabeschannel", "eurofoxes", "everythingbutt", "evilangel", "evilshow", "exotic4k", "exploitedcollegegirls", "extremepickups",
    "exxxtrasmall", "facialfest", "fakeagent", "fakeagentuk", "fakecop", "fakedrivingschool", "fakehospital", "fakeshooting",
    "faketaxi", "familiestied", "familysinners", "familystrokes", "familystrokesfeatures", "familyswap", "familyxxx", "farthammer",
    "feedherfuckher", "feedme", "femalesubmission", "fetishmodelpupett", "fillupymom", "filthsyndicate", "filthyfamily", "firstclasspov",
    "firsttimefootsmellers", "fit18", "fitnessrooms", "flatandfuckedmilfs", "footjobfantasiescumtrue", "footsiebabes", "footworship", "forbiddenseductions",
    "fostertapes", "freaksinside", "freakyfembots", "freeusefantasy", "freeusesingles", "freeze", "fuckedandbound", "fuckfordollars",
    "fuckingmachines", "fuckmyass", "fuckpassvr", "fuckstudies", "fuckteamfive", "futuredarkly", "gayrevenge", "gfflive",
    "gfforiginals", "gingerpatch", "girlcore", "girlfriendsfilms", "girlgirl", "girlgirlxxx", "girlsgonecrazy", "girlsgonepink",
    "girlsunderarrest", "girlsway", "girlswholie", "girlswild", "gloryhole", "gloryholeloads", "glowupz", "goddessnudes",
    "grinders", "handjobhq", "hardcoregangbang", "hardkinks", "hardx", "harmonyfetish", "hazeher", "hegre",
    "hegreart", "hentaisexschool", "herfirstlesbiansex", "herfreshmanyear", "heteroflexible", "heyzo", "hiddenlayers", "hijabhookup",
    "hogtied", "hologirlsvr", "homealonemilfs", "hornybirds", "hotbabes4k", "hotkinkyjoxxx", "hotkinkyme", "housewife1on1",
    "howmenorgasm", "hussieauditions", "hussiepass", "hustler", "hustlerparodies", "hypkinkseductionispower", "iconmale", "ifilmmyself",
    "ihaveawife", "iknowthatgirl", "iloveblackshemales", "indianbabes", "innocenthigh", "internalviolations", "intimatelypov", "isthisreal",
    "iwantmysister", "javhd", "javhub", "jaxslayher", "jizzonmyjugs", "joibabes", "joimom", "jordi",
    "joymii", "juicypinkbox", "julesjordan", "karupsprivatecollection", "kink", "kinkyangels", "kinkybites", "kinkybitesmen",
    "kinkydogs", "kinkyexploits", "kinkyfamily", "kinkyfeetures", "kinkygirlsberlin", "kinkyinlaws", "kinkykingdom", "kinkyleatherclips",
    "kinkymarisol", "kinkymistresses", "kinkypanthersbdsm", "kinkypleasures", "kinkyponygirl", "kinkyrubberworld", "kinkysex", "kinkysluts4k",
    "kinkyspa", "kinkytwink", "kinkyvisions", "kissingsis", "ladygonzo", "lasluts", "latexplaytime", "latinachannel",
    "latinamilf", "latinarampage", "latinasextapes", "latinastepmom", "latinateam", "legalporno", "legsex", "lesbiancrimestories",
    "lesbianghoststories", "lesbiangirlongirl", "letsbebad", "letspostit", "letstryanal", "lezbehonest", "lifeselector", "lild",
    "lillatinas", "lilsis", "littleasians", "livenaughtystudent", "livingwithanna", "lovehairy", "lusthd", "magicalfeet",
    "massagecreep", "mature4k", "maturenl", "meanbitch", "meninpain", "menonedge", "messyjessy", "metart",
    "metartx", "metmodels", "michaelninn", "mikesapartment", "milehighmedia", "milehighxtreme", "milfed", "milfhunter",
    "milflessons", "milfslikeitbig", "milfslikeitblack", "milfsoup", "milfsugarbabesclassic", "milftugs", "milfy", "milkenema",
    "milkybabes", "mixedx", "mofos", "momcomesfirst", "momdrips", "momisamilf", "momishorny", "momknowsbest",
    "mommy4k", "mommygotboobs", "mommysboy", "mommystoytime", "momsanaladventure", "momsbangteens", "momsincontrol", "momslickteens",
    "momsmoney", "momsteachsex", "momwantscreampie", "momwantstobreed", "mondofetiche", "moneytalks", "monstercurves", "monstersofcock",
    "mormongirlz", "motherdaughterexchangeclub", "mplstudios", "mranal", "mrcameltoe", "muramura", "mybabysittersclub", "mydirtymaid",
    "mydirtyuncle", "mydirtyvault", "myfamilypies", "myfirst", "myfirstsexteacher", "myfriendsfeet", "myfriendshotgirl", "myfriendshotmom",
    "mygf", "mykinkydope", "mylf", "mylfxevilangel", "mylfxteamskeet", "mylifeinbrazil", "mypornbabes", "nakedhustlers",
    "nakedkombat", "nakedyogalife", "naughtyamerica", "naughtyamericavr", "newbieblack", "newsensations", "nfbusty", "nofaces",
    "noirmale", "notmygrandpa", "nubilescasting", "nubileset", "nubilesnet", "nubilesporn", "nubilesunscripted", "officeobsession",
    "oldhornymilfs", "onlyfans", "onlytarts", "oopsfamily", "oopsie", "oopsieanimated", "openfamily", "ourlittlesecret",
    "oyeloca", "pacopacomama", "pansexualx", "papaloads", "partygirls", "partyof3", "passion", "passion4k",
    "passionhd", "passportbros", "penthouse", "penthousegold", "perfectfuckingstrangersclassic", "perspective", "pervdoctor", "pervdriver",
    "pervmassage", "pervmom", "pervtherapy", "petiteballerinasfucked", "petitehdporn", "petiteteens18", "pickinguppussy", "pinko",
    "plumperpass", "polyfamilylife", "pornfidelity", "pornmegaload", "pornpros", "pornstarplatinumkink", "pornstarslikeitbig", "pornstarspa",
    "pornstarspunishment", "pornstarvote", "pornstarwife", "pov4k", "povfantasy", "povlife", "povmassage", "povmasters",
    "povpickups", "powermunch", "preggoworld", "prettydirty", "princesscum", "private", "privateblack", "privatesociety",
    "projectdtf", "projectrv", "propertysex", "puba", "pubamedia", "publicagent", "publicbang", "publicdisgrace",
    "publicinvasion", "publicpickups", "punishteens", "pure18", "puretaboo", "purets", "pussypatrol", "pvcbabes",
    "queensofkink", "randysroadstop", "realbutts", "realexgirlfriends", "realfuckingcouples", "realgirlsnow", "realityjunkies", "realitykings",
    "realitysis", "realjamvr", "realpornstarsvr", "realwifestories", "rickysroom", "rkprime", "roccosiffredi", "roundandbrown",
    "rubateen", "russianfakeagent", "sadisticrope", "savagegangbang", "sayunclexpakinky", "scalebustinbabes", "scorehd", "scoreland",
    "scoreland2", "secretcrush", "seducedbyacougar", "selfdesire", "sexandgrades", "sexandsubmission", "sexart", "sexbusters",
    "sexlikereal", "sexmex", "sexselector", "sexybabes", "sexyclubbabes", "sexysandeecom", "shanedieselsbanginbabes", "shapeofbeauty",
    "sharizelvideos", "shesbreedingmaterial", "shesnew", "shewantshim", "shoplyfter", "showersolos", "showmybf", "singlemoms",
    "sinsvr", "sislovesme", "sisswap", "sistertrick", "slayed", "sleazystepdad", "slroriginals", "slutstepmom",
    "slutstepsister", "sluttywhitegirls", "smashed", "sneakysex", "solointerviews", "spanish18", "spankmonster", "spermglazed",
    "spizoo", "sportbabes", "springbreak", "stayhomepov", "stepfamilychannel", "stepmomlessons", "stepmomvideos", "stepsiblings",
    "stepsiblingscaught", "stockingsvr", "straplez", "streetranger", "strugglingbabes", "stunning18", "submissived", "sugarbabestv",
    "summervacation", "supersluts", "swallowbay", "sweetheartvideo", "sweetsinner", "takenrough", "teacherfucksteens", "teamskeet",
    "teenagelesbian", "teencurves", "teenfidelity", "teenjoi", "teenoverload", "teenpies", "teenpinkvideos", "teensatwork",
    "teensdoporn", "teenslikeitbig", "teenslikeitblack", "teensloveanal", "teensloveblackcocks", "teenslovecream", "teenslovehugecocks", "teenslovemoney",
    "teenyblack", "telsev", "tgirlpornstar", "thebrats", "thefootinfatuation", "thegroupexperiment", "thekinkfaeriex", "thelifeerotic",
    "theloft", "therealworkout", "thescoregroup", "thesexscout", "thespa", "thetrainingofo", "theupperfloor", "theyeslist",
    "thickumz", "thisgirlsucks", "thundercock", "tickleaddiction", "tinysis", "titsandtugs", "tittyattack", "tokyohot",
    "tomboyish", "tomboyz", "tonightsfuck", "tonightsgirlfriend", "tonightsts", "toywithme", "trannysurprise", "trannytemptation",
    "transfixed", "transfixedoriginale", "transgressivefilms", "transsensual", "transsexualangel", "trophywives", "truelesbian", "truesexstories",
    "tsdivas", "tsfactor", "tskink", "tsplayground", "tspussyhunters", "tsseduction", "tugjobs", "turningtwistys",
    "tushy", "tushyraw", "twistys", "twistyshard", "twistysteasers", "uksoccerbabes", "ultimatesurrender", "underthebed",
    "unrelatedx", "upclosex", "vengeancexxx", "virtualporn", "virtualrealporn", "virtualtaboo", "vivid", "vividalt",
    "vividceleb", "vividclassic", "vivthomas", "vixen", "vixenmediagroup", "vrbangers", "vrconk", "vrcosplayx",
    "w4b", "wankzvr", "wasteland", "watch4beauty", "watchingmydaughtergoblack", "watchingmymomgoblack", "watchyourmom", "waterbondage",
    "wefuckblackgirls", "welivetogether", "wetforwomen", "whengirlsplay", "wheretheboysarent", "whippedass", "wicked", "wickedpictures",
    "wifewriting", "wifey", "wiredpussy", "wivesonvacation", "wolfwagner", "womenseekingwomen", "womensworld", "woodmanentertainment",
    "workinglatinas", "wowgirls", "wrestlingmale", "xandercorvus", "xlgirls", "xplumper", "xxxpawn", "xvideosred", "youngandcurious",
    "youngermommy", "youngkink", "yourwifemymeat", "zebragirls", "zerotolerance", "zerotolerancefilms",
}

# Curated minimal baseline of standard studio/release-group abbreviations
_STUDIO_ABBREVIATIONS = {
    "prvs": "PrivateSociety",
    "fpv": "FuckPassVR",
    "lp": "LegalPorno",
    "rk": "RealityKings",
    "dp": "DigitalPlayground",
    "ea": "EvilAngel",
    "jj": "JulesJordan",
    "fstv": "FamilyStrokes",
    "mfp": "MyFamilyPies",
    "mpf": "MyPervyFamily",
    "bs": "BrattySis",
    "ms": "MomSwap",
    "ta": "TushyRaw",
    "mfs": "MomsFamilySecrets",
    "hmf": "HotMilfsFuck",
}

# ── Advanced Date & Site extractors for Western Adult filename parsing ─────────

def extract_date(text: str) -> Optional[Tuple[str, int, int]]:
    # Try YYYY-MM-DD or YYYY.MM.DD or YYYY_MM_DD
    m = re.search(r'\b(?P<year>(?:19|20)\d{2})[.\-_ ]+(?P<month>0[1-9]|1[0-2])[.\-_ ]+(?P<day>0[1-9]|[12]\d|3[01])\b', text)
    if m:
        return f"{m.group('year')}-{m.group('month')}-{m.group('day')}", m.start(), m.end()

    # Try DD-MM-YYYY or DD.MM.YYYY or DD_MM_YYYY
    m = re.search(r'\b(?P<day>0[1-9]|[12]\d|3[01])[.\-_ ]+(?P<month>0[1-9]|1[0-2])[.\-_ ]+(?P<year>(?:19|20)\d{2})\b', text)
    if m:
        return f"{m.group('year')}-{m.group('month')}-{m.group('day')}", m.start(), m.end()

    # Try YY-MM-DD or YY.MM.DD or YY_MM_DD
    m = re.search(r'\b(?P<year>\d{2})[.\-_ ]+(?P<month>0[1-9]|1[0-2])[.\-_ ]+(?P<day>0[1-9]|[12]\d|3[01])\b', text)
    if m:
        year = int(m.group('year'))
        year_str = f"20{year:02d}" if year < 50 else f"19{year:02d}"
        return f"{year_str}-{m.group('month')}-{m.group('day')}", m.start(), m.end()

    return None


def extract_studio(text: str) -> Optional[Tuple[str, str]]:
    """
    Find if any known studio name exists in the text.
    Returns (matched_studio_key, original_site_substring) or None.
    Prioritizes the match that appears earliest in the text (lowest start index).
    If multiple matches start at the same index, the longer one wins.
    """
    matches = []
    for studio in _NSFW_STUDIOS:
        pattern = '[.\\-_ ]*'.join(re.escape(c) for c in studio)
        m = re.search(pattern, text, re.IGNORECASE)
        if m:
            start_idx = m.start()
            matches.append((start_idx, len(studio), studio, m.group(0)))
            
    if not matches:
        return None
        
    # Sort matches:
    # 1. By start index ascending (earliest match wins)
    # 2. By length of studio key descending (longer match wins for same start index)
    matches.sort(key=lambda x: (x[0], -x[1]))
    best_match = matches[0]
    return best_match[2], best_match[3]


def parse_adult_filename(title: str) -> dict:
    """
    Parse an adult torrent name into {site, date, name, performer}.
    """
    stem = re.sub(r'\.(mp4|mkv|avi|mov|wmv|webm|ts)$', '', title, flags=re.I)
    
    result = {"site": None, "date": None, "name": None, "raw": title, "performer": None}

    # 1. Try to extract date
    date_info = extract_date(stem)
    if date_info:
        date_str, d_start, d_end = date_info
        result["date"] = date_str
        stem_no_date = stem[:d_start] + " " + stem[d_end:]
    else:
        stem_no_date = stem
    
    # 2. Check for site - performer - scene structure (split by double hyphen ' - ')
    parts = [p.strip() for p in re.split(r'\s+-\s+', stem_no_date)]
    if len(parts) >= 3:
        # e.g. Site - Performer - Title
        site_candidate = parts[0]
        perf_candidate = parts[1]
        scene_candidate = " - ".join(parts[2:])
        
        # Clean them
        result["site"] = name_cleaner(site_candidate).strip()
        result["performer"] = name_cleaner(perf_candidate).strip()
        result["name"] = name_cleaner(scene_candidate).strip()
    elif len(parts) == 2:
        # e.g. Site - Performer or Site - Scene
        site_candidate = parts[0]
        right_candidate = parts[1]
        
        result["site"] = name_cleaner(site_candidate).strip()
        cleaned_right = name_cleaner(right_candidate).strip()
        
        # If the right side is short (<= 3 words), it's highly likely to be a performer name
        if len(cleaned_right.split()) <= 3:
            result["performer"] = cleaned_right
            result["name"] = cleaned_right
        else:
            result["name"] = cleaned_right
    else:
        # Fall back to standard extraction
        # Try to extract known studio
        studio_info = extract_studio(stem_no_date)
        if studio_info:
            studio_key, orig_site = studio_info
            result["site"] = orig_site.strip(" -._")
            stem_no_date = stem_no_date.replace(orig_site, " ")
            
        # Clean up remaining stem for scene name
        cleaned_name = name_cleaner(stem_no_date)
        cleaned_name = re.sub(r'^[-\s._()]+', '', cleaned_name)
        cleaned_name = re.sub(r'[-\s._()]+$', '', cleaned_name)
        result["name"] = cleaned_name if cleaned_name else None

    # Set performer in fallback cases if name is extremely short (1 to 3 words)
    if not result["performer"] and result["name"] and len(result["name"].split()) <= 3:
        result["performer"] = result["name"]

    # Normalize the site name if we have abbreviations (e.g. prvs -> PrivateSociety)
    if result["site"]:
        site_lower = result["site"].lower().strip()
        if site_lower in _STUDIO_ABBREVIATIONS:
            result["site"] = _STUDIO_ABBREVIATIONS[site_lower]

    log(f"Parsed filename → site={result['site']!r} date={result['date']!r} name={result['name']!r} performer={result['performer']!r}")
    return result


def studio_in_title(title: str) -> bool:
    """Return True only if a known adult studio name appears in the title."""
    t_no_sep = re.sub(r'[^a-z0-9]', '', title.lower())
    return any(s in t_no_sep for s in _NSFW_STUDIOS)

_DATE_RE = re.compile(
    r'\b(?:'
    r'(?:19|20)\d{2}[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:0[1-9]|[12]\d|3[01])|'  # YYYY-MM-DD
    r'(?:0[1-9]|[12]\d|3[01])[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:19|20)\d{2}|'  # DD-MM-YYYY
    r'\d{2}[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:0[1-9]|[12]\d|3[01])'            # YY-MM-DD
    r')\b'
)

def is_adult_content(title: str) -> bool:
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
    r'(?:^|[^a-zA-Z0-9])([A-Z]{2,6})[-_ ]?(\d{2,5})(?:[^a-zA-Z0-9]|$)',
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
        
        # Check if the number is part of a date (e.g. YY-MM-DD or YYYY-MM-DD)
        remaining = stem[m.start(2):]
        if re.match(r'^\d{2,4}[-._]\d{2}[-._]\d{2}', remaining):
            return None

        if prefix.lower() == "vr" and number in ("180", "360"):
            return None
        if prefix.lower() in _NOT_JAV_PREFIXES:
            return None
        # Reject if the prefix is a known western adult studio
        if prefix.lower() in _NSFW_STUDIOS:
            return None
        # Reject if the whole stem is just the "code" and it looks like a season tag
        if re.match(r'^S\d{1,2}$', prefix, re.I):
            return None
        # Reject if the number looks like a year (e.g. 1980-2035)
        if len(number) == 4 and 1980 <= int(number) <= 2035:
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
        r = SESSION.get(url, headers=_TPDB_HEADERS(), timeout=5)
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
    return _normalise_tpdb(d, source="tpdb_jav")


# ── Fuzzy matching (namer-style) ───────────────────────────────────────────────
def is_subsequence(sub: str, string: str) -> bool:
    if not sub or not string:
        return False
    it = iter(string.lower())
    return all(c in it for c in sub.lower())

def fuzzy_score(query: Optional[str], candidate: str) -> float:
    if not query or not candidate:
        return 0.0
    if HAS_RAPIDFUZZ:
        q_proc = rfutils.default_process(query)
        c_proc = rfutils.default_process(candidate)
        if not q_proc or not c_proc:
            return 0.0
        
        base_score = fuzz.WRatio(q_proc, c_proc)
        
        # If candidate is very short (e.g. < 15 characters), restrict loose partial matches
        if len(candidate) < 15:
            # Require a decent overall ratio match to prevent matching a single word in a long query
            ratio_score = fuzz.ratio(q_proc, c_proc)
            partial_ratio = fuzz.partial_ratio(q_proc, c_proc)
            # If the overall ratio and partial ratio are both low, heavily penalize WRatio
            if ratio_score < 40 and partial_ratio < 75:
                base_score = min(base_score, ratio_score * 1.5)
                
        return base_score
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

def is_abbreviation(abbrev: str, full_name: str) -> bool:
    """
    Check if a short string (abbrev) is an abbreviation of a full name (e.g. 'fpv' -> 'FuckPassVR').
    Typically works by taking the first letters of capitalized words or space/separator-split words.
    """
    if not abbrev or not full_name:
        return False
    abbrev = abbrev.lower().strip()
    if not abbrev or len(abbrev) > 6:
        return False
        
    full_name_clean = unidecode(full_name).lower()
    full_name_no_sep = re.sub(r'[^a-z0-9]', '', full_name_clean)
    
    # 1. Check direct map
    mapped = _STUDIO_ABBREVIATIONS.get(abbrev)
    if mapped:
        mapped_clean = mapped.lower()
        if mapped_clean in full_name_no_sep or full_name_no_sep in mapped_clean:
            return True
            
    # Split by spaces and separators
    words = re.findall(r'[a-z0-9]+', full_name_clean)
    if not words:
        return False
        
    # Heuristic 1: First letter of each word (e.g. 'rk' -> 'reality kings', 'fstv' -> 'family strokes')
    first_letters = "".join(w[0] for w in words)
    if abbrev == first_letters:
        return True
        
    # Heuristic 2: Capital letters in a camelCase/PascalCase string (e.g. 'FuckPassVR' -> F, P, V, R -> fpvr or fpv)
    caps = "".join(c.lower() for c in full_name if c.isupper())
    if caps and abbrev in caps:
        return True
        
    # Heuristic 3: Substring of first letters
    if len(abbrev) >= 2 and abbrev in first_letters:
        return True
        
    # Heuristic 4: Subsequence matching for abbreviations >= 3 characters
    if len(abbrev) >= 3 and is_subsequence(abbrev, full_name_no_sep):
        return True
        
    return False

def score_result(parsed: dict, scene: dict, target_duration: Optional[float] = None) -> float:
    """
    Score a TPDB scene against parsed filename parts.
    Mirrors namer's __match_weight logic:
      site match       → +100
      date match       → +100
      name fuzzy       → 0-100
      performer boost  → +150 (if a performer name is in the filename)
      duration match   → +300 (diff < 1s) or +200 (diff < 3s)
    """
    score = 0.0

    # 1. Dynamic Site Recognition if parsed["site"] is None
    # Check if normalized candidate site/network/parent name is a substring of the raw filename
    if not parsed.get("site"):
        raw_clean = re.sub(r'[^a-z0-9]', '', unidecode(parsed.get("raw") or "").lower())
        for field in ["site", "parent", "network", "studio", "brand"]:
            val = scene.get(field)
            if val:
                val_clean = re.sub(r'[^a-z0-9]', '', unidecode(val).lower())
                # Ignore very short site names to avoid false positives (must be >= 4 chars)
                if len(val_clean) >= 4 and val_clean in raw_clean:
                    parsed["site"] = val
                    break

    # 2. Pre-calculate Platform and Performer Match
    scene_site_clean = re.sub(r'[^a-z0-9]', '', unidecode(scene.get("site") or "").lower())
    parsed_site_clean = re.sub(r'[^a-z0-9]', '', unidecode(parsed.get("site") or "").lower()) if parsed.get("site") else ""
    # Generic UGC/creator platforms where performer identity is critical for matching.
    # Use exact match to avoid false positives (e.g. "xvideosred" is NOT "xvideos").
    _GENERIC_PLATFORMS = {"onlyfans", "fansly", "manyvids", "fansdb", "patreon", "fans", "xvideos", "pornhub", "spankbang", "redtube", "tube"}
    is_platform = (parsed_site_clean in _GENERIC_PLATFORMS or any(parsed_site_clean == p for p in _GENERIC_PLATFORMS)) or \
                  any(scene_site_clean == p or scene_site_clean.startswith(p + ":") for p in _GENERIC_PLATFORMS)

    perf_match = False
    raw_title = parsed.get("raw", "").lower()
    
    # Check explicitly extracted performer first
    if parsed.get("performer"):
        parsed_perf_clean = re.sub(r'[^a-z0-9]', '', unidecode(parsed["performer"]).lower())
        for p in scene.get("performers") or []:
            name = p.get("name") or (p.get("performer") or {}).get("name")
            if name:
                name_clean = re.sub(r'[^a-z0-9]', '', unidecode(name).lower())
                if parsed_perf_clean and (parsed_perf_clean in name_clean or name_clean in parsed_perf_clean or fuzzy_score(parsed["performer"], name) >= 85.0):
                    perf_match = True
                    break
        
        # Fallback: check if the parsed performer name is in the candidate scene title
        if not perf_match and scene.get("title"):
            title_clean = re.sub(r'[^a-z0-9]', '', unidecode(scene["title"]).lower())
            if len(parsed_perf_clean) >= 4 and parsed_perf_clean in title_clean:
                perf_match = True

    if not perf_match:
        for p in scene.get("performers") or []:
            name = p.get("name") or (p.get("performer") or {}).get("name")
            if name:
                name_lower = name.lower()
                # If the performer name is the same as the parsed site (e.g. Jules Jordan), skip it for performer boost
                perf_clean = re.sub(r'[^a-z0-9]', '', unidecode(name_lower).lower())
                if parsed_site_clean and (parsed_site_clean == perf_clean or perf_clean in parsed_site_clean or parsed_site_clean in perf_clean):
                    continue
                    
                if name_lower in raw_title:
                    perf_match = True
                    break
                cleaned_perf = re.sub(r'[^a-z0-9]', '', name_lower)
                cleaned_raw = re.sub(r'[^a-z0-9]', '', raw_title)
                if cleaned_perf in cleaned_raw:
                    perf_match = True
                    break

    # Check if the query parsed name is ONLY the performer's name
    is_only_performer = False
    if parsed.get("name"):
        for p in scene.get("performers") or []:
            p_name = p.get("name") or (p.get("performer") or {}).get("name")
            if p_name:
                if fuzzy_score(parsed["name"], p_name) >= 90.0:
                    is_only_performer = True
                    break

    # 3. Site match / mismatch penalty (including parent/network sites)
    site_matched = False
    if parsed.get("site"):
        parsed_site = re.sub(r'[^a-z0-9]', '', unidecode(parsed["site"]).lower())
        is_whitelisted = parsed_site in _NSFW_STUDIOS
        
        # Check direct site match first
        scene_site = re.sub(r'[^a-z0-9]', '', unidecode(scene.get("site") or "").lower())
        if parsed_site and scene_site and (parsed_site in scene_site or scene_site in parsed_site or is_abbreviation(parsed["site"], scene.get("site"))):
            score += 120 if is_whitelisted else 80  # Direct site match premium boost for whitelist, moderate for dynamic
            site_matched = True
        else:
            # Check parent/network matches
            scene_sites = []
            if scene.get("parent"):
                scene_sites.append(re.sub(r'[^a-z0-9]', '', unidecode(scene["parent"]).lower()))
            if scene.get("network"):
                scene_sites.append(re.sub(r'[^a-z0-9]', '', unidecode(scene["network"]).lower()))
                
            for s_name in scene_sites:
                if s_name and (parsed_site in s_name or s_name in parsed_site):
                    site_matched = True
                    break
                    
            if site_matched:
                score += 100 if is_whitelisted else 60  # Network/parent match premium boost for whitelist, moderate for dynamic
            else:
                if not is_platform:
                    # Hybrid site mismatch penalty:
                    # Harsher penalty (-180) if parsed site is in the NSFw whitelist,
                    # softer penalty (-80) if dynamically guessed.
                    # Relieved to -80 if both performer and date match exactly!
                    has_strong_performer_and_date = perf_match and parsed.get("date") and scene.get("date") and parsed["date"] == scene["date"][:10]
                    score -= 180 if (is_whitelisted and not has_strong_performer_and_date) else 80
    else:
        # No site info in filename — don't penalise
        score += 50

    # 3.2 Platform-compatible site matching:
    # "OnlyFans" in filename + scene site is "FansDB: Creator (onlyfans)" → compatible, soft boost.
    # This handles the common case where FansDB (StashDB) indexes OnlyFans/Fansly content.
    if not site_matched and is_platform and parsed_site_clean and parsed.get("site"):
        scene_site_raw_lower = (scene.get("site") or "").lower()
        if parsed_site_clean in scene_site_raw_lower:
            site_matched = True
            score += 50  # Soft platform-compatible match (less than a direct site match)

    # 3.5 Creator/Performer match validation for generic platforms
    # If the candidate scene is on a generic platform creator page,
    # the creator/performer's name MUST be present in the raw filename!
    # If none of the candidate scene's performers are mentioned in the raw filename,
    # and the site is a generic platform, apply a severe penalty of -300.
    if is_platform and not perf_match:
        # Check if the creator site name itself is in the raw filename (e.g. "OnlyFans - Cami Strella")
        # FansDB scene sites look like "FansDB: CreatorName (onlyfans)" — strip the parenthetical
        # platform suffix before extracting the creator name, otherwise we check for
        # "creatornameonlyfans" instead of "creatorname" and always miss.
        has_creator_in_filename = False
        site_str = scene.get("site") or ""
        parts = re.split(r'[:\-]', site_str)
        if len(parts) >= 2:
            creator_part = re.sub(r'\(.*?\)', '', parts[1]).strip()  # strip (onlyfans), (fansly) etc.
            creator_name = re.sub(r'[^a-z0-9]', '', unidecode(creator_part).lower())
            raw_clean = re.sub(r'[^a-z0-9]', '', unidecode(parsed.get("raw") or "").lower())
            if len(creator_name) >= 3 and creator_name in raw_clean:
                has_creator_in_filename = True
                
        if not has_creator_in_filename:
            score -= 300.0

    # 4. Date match / mismatch
    if parsed.get("date"):
        if scene.get("date"):
            # Relieve date penalty ONLY if we have a strong performer and site match AND it's not a generic performer-name-only filename
            has_strong_match = perf_match and site_matched and not is_only_performer

            try:
                p_date_str = parsed["date"]
                s_date_str = scene["date"][:10]
                p_date = datetime.strptime(p_date_str, "%Y-%m-%d").date()
                s_date = datetime.strptime(s_date_str, "%Y-%m-%d").date()
                diff_days = abs((p_date - s_date).days)
                if diff_days <= 1:
                    score += 100
                elif diff_days <= 2:
                    score += 50
                elif diff_days <= 7:
                    # Close enough — could be a timezone edge, delayed publish, or index lag
                    score += 20
                else:
                    # Large date mismatch — apply tiered penalty based on match strength
                    if perf_match and site_matched:
                        if is_only_performer:
                            # Performer-name-only queries: filename date is often a download/upload
                            # date, not release date — very lenient penalty
                            score -= 15
                        else:
                            score -= 50
                    else:
                        t_score = fuzzy_score(parsed.get("name"), scene.get("title"))
                        if t_score >= 85.0 and not is_only_performer:
                            score -= 50
                        else:
                            score -= 100
            except Exception:
                if parsed["date"] == scene["date"][:10]:
                    score += 100
                else:
                    if perf_match and site_matched:
                        score -= 15 if is_only_performer else 50
                    else:
                        t_score = fuzzy_score(parsed.get("name"), scene.get("title"))
                        if t_score >= 85.0 and not is_only_performer:
                            score -= 50
                        else:
                            score -= 100
        else:
            # Filename has a date but database scene does not — do not penalise, but no boost
            pass
    else:
        score += 50  # no date info — don't penalise

    # 5. Apply performer match boost
    if perf_match:
        score += 150

    # 6. Name fuzzy match against scene title only
    if parsed.get("name") and scene.get("title"):
        title_lower = scene["title"].lower()
        
        # Check if the scene title is just a performer's name
        is_perf_title = False
        for p in scene.get("performers") or []:
            p_name = p.get("name") or (p.get("performer") or {}).get("name")
            if p_name and p_name.lower() == title_lower:
                is_perf_title = True
                break
                
        if is_perf_title:
            # Check if query has extra words not in the performer name
            q_words = set(re.findall(r'[a-z0-9]+', parsed["name"].lower()))
            t_words = set(re.findall(r'[a-z0-9]+', title_lower))
            extra_words = q_words - t_words - {"ly", "rq", "mp4", "mkv", "avi", "wmv", "ts"}
            if len(extra_words) >= 2:
                score -= 150.0  # Apply strong penalty since filename has a scene title but candidate is a performer profile

        # Check for unmatched scene title words (extra words in query that are not in candidate title, performers, nor site/studio/network/parent names)
        if parsed.get("name") and scene.get("title"):
            q_words = set(re.findall(r'[a-z0-9]{3,}', parsed["name"].lower()))
            t_words = set(re.findall(r'[a-z0-9]{3,}', scene["title"].lower()))
            
            p_words = set()
            for p in scene.get("performers") or []:
                name = p.get("name") or (p.get("performer") or {}).get("name")
                if name:
                    p_words.update(re.findall(r'[a-z0-9]{3,}', name.lower()))
            
            # Support matching query words against the candidate's actual site/studio/network/parent names
            s_words = set()
            for field in ["site", "parent", "network", "studio", "brand"]:
                val = scene.get(field)
                if val:
                    s_words.update(re.findall(r'[a-z0-9]{3,}', val.lower()))
            
            # Common stop words, technical resolution formats, and common release group tags to ignore
            stop_words = {
                "the", "and", "for", "with", "you", "your", "that", "this", "from", "her", "him", "she", "his", "out", "our", "all", "its", "under",
                "1080p", "2160p", "4096p", "4k", "8k", "hd", "fhd", "sd", "mp4", "mkv", "avi", "wmv", "mov", "webm", "ts", "vr180", "xxx", "hevc", "x264", "x265", "h264", "h265",
                "p2p", "xc", "wrb", "vsex", "rarbg", "yify", "eztv", "fgt", "narcos", "prt", "vol", "ch", "ppv",
                "had", "has", "have", "was", "were", "are", "is", "get", "gets", "got", "take", "takes", "took", "give", "gives", "gave", "make", "makes", "made", "come", "comes", "came", "go", "goes", "went", "do", "does", "did", "new", "old", "big", "small", "one", "two", "three", "first", "last", "just", "about", "some", "like", "how", "why", "who", "what", "where", "when", "can", "could", "would", "should", "will", "shall", "may", "might", "must", "but", "not", "too", "very", "much", "many", "more", "most", "few", "less", "least", "own", "other", "same", "different", "good", "bad", "hot", "cool", "warm", "cold", "now", "then", "once", "twice", "here", "there", "every", "each", "both", "either", "neither", "any", "some", "none", "only", "well", "done"
            }
            
            # Advanced substring/compound word matching:
            # For each word in q_words, if it is a substring of (or contains as a substring) any candidate field, ignore it!
            all_candidate_words = t_words | p_words | s_words | stop_words
            
            extra_words = set()
            for qw in q_words:
                if qw in stop_words:
                    continue
                # Ignore year digits (4-digit numbers starting with 19 or 20)
                if qw.isdigit() and len(qw) == 4 and (qw.startswith("19") or qw.startswith("20")):
                    continue
                matched = False
                for cw in all_candidate_words:
                    if qw == cw or qw in cw or cw in qw:
                        matched = True
                        break
                if not matched:
                    extra_words.add(qw)
            
            # Check how many actual non-stop title keywords they share outside performer/site names
            shared_title_words = (q_words & t_words) - p_words - s_words - stop_words
            
            # If they share at least 2 non-stop title words, or if they share at least 1 and have a strong performer/site match,
            # we skip the severe penalty (reducing it to a minor penalty or completely skipping it).
            has_shared_title = len(shared_title_words) >= 2 or (len(shared_title_words) >= 1 and (perf_match or site_matched))
            
            if len(extra_words) >= 2:
                if is_platform:
                    score -= 10.0  # Very minor penalty for platforms due to descriptive raw filenames
                elif is_only_performer and perf_match and site_matched:
                    pass  # Performer-name-only query with confirmed site+perf match: skip word penalty
                else:
                    # Dynamic unmatched words penalty scaled by word count:
                    # -60 per word, capped at -150 max. Halved if they share keywords.
                    # Also halved when performer match is confirmed (performer name adds context).
                    base_mult = 30.0 if (has_shared_title or perf_match) else 60.0
                    base_cap  = 75.0 if (has_shared_title or perf_match) else 150.0
                    word_penalty = min(base_cap, len(extra_words) * base_mult)
                    score -= word_penalty

        # Only add unique scene title fuzzy score if the query name is not purely a performer's name
        if not is_only_performer:
            name_score = fuzzy_score(parsed["name"], scene["title"])
            score += name_score

            # Prevent mismatches (false positives) when the scene title is specified in the query
            # but is completely different from the candidate title.
            # We require either:
            # - At least one shared non-generic keyword (allowing compound/substring matches)
            # - At least 3 shared keywords total (including generic ones)
            # - At least 60% of the query keywords are shared
            # - A strong duration match (diff <= 30s) if duration is available
            q_all = set(re.findall(r'[a-z0-9]+', parsed["name"].lower()))
            t_all = set(re.findall(r'[a-z0-9]+', scene["title"].lower()))

            allowed_2_letter = {"dp", "bj", "xx", "vr"}
            q_words = {w for w in q_all if (len(w) >= 3 or w in allowed_2_letter)} - stop_words
            t_words = {w for w in t_all if (len(w) >= 3 or w in allowed_2_letter)} - stop_words

            # Filter out year digits or pure numbers from keywords to avoid false matches on years/resolutions
            q_words = {w for w in q_words if not (w.isdigit() and len(w) == 4)}
            t_words = {w for w in t_words if not (w.isdigit() and len(w) == 4)}

            shared_words = set()
            for qw in q_words:
                for cw in t_words:
                    if qw == cw or qw in cw or cw in qw:
                        shared_words.add(qw)
                        break

            generic_keywords = {
                # Common verbs & action words
                "fuck", "fucks", "fucked", "fucking", "suck", "sucks", "sucked", "sucking",
                "blowjob", "bj", "bjs", "bj's", "anal", "creampie", "cum", "cums", "cumming", "swallow",
                "swallows", "swallowed", "ride", "rides", "riding", "facial", "facials",
                "fist", "fisting", "peg", "pegging", "strip", "strips", "stripping",
                "masturbate", "masturbating", "jerk", "jerking", "squirt", "squirting",
                "lick", "licks", "licking", "fuckboys", "fuckboy", "cuck", "cuckold", "cucks",
                # Common descriptors and adjectives
                "cute", "hot", "sexy", "gorgeous", "beautiful", "pretty", "busty", "petite",
                "blonde", "brunette", "ebony", "asian", "latina", "teen", "milf", "milfs",
                "new", "old", "first", "last", "good", "bad", "big", "small", "hard", "soft",
                "raw", "real", "fake", "dirty", "clean", "bored", "lazy", "natural", "wild",
                # Roles and relations
                "step", "stepmom", "stepsis", "sister", "brother", "stepbrother", "dad", "mom",
                "stepdaughter", "stepson", "stepsister", "daddy", "mommy", "wife", "husband",
                "girlfriend", "boyfriend", "friend", "friends", "roommate", "roommates",
                "landlord", "boss", "maid", "nurse", "barista", "girl", "girls", "guy", "guys",
                # Common nouns/formats and time of day
                "video", "videos", "scene", "scenes", "drop", "drops", "trailer", "trailers",
                "homemade", "show", "shows", "clip", "clips", "preview", "previews",
                "teaser", "teasers", "brand", "part", "episode", "vol", "volume", "ch", "chapter",
                "exclusive", "exclusives", "special", "specials", "anniversary", "update",
                "orgy", "threesome", "dp", "double", "penetration", "mmf", "ffm",
                "sextape", "tape", "tapes", "pov", "bts", "behind", "cast", "casting",
                "couch", "audition", "auditions", "interview", "interviews", "live", "stream",
                "livestream", "footage", "hauls", "haul",
                "night", "day", "morning", "afternoon", "evening", "today", "yesterday"
            }

            shared_non_generic = shared_words - generic_keywords

            has_duration_match = False
            if target_duration is not None and scene.get("duration"):
                try:
                    diff = abs(float(scene["duration"]) - target_duration)
                    if diff <= 30.0:
                        has_duration_match = True
                except (ValueError, TypeError):
                    pass

            # Check if there is a legitimate match based on word sharing or duration
            has_legitimate_match = False
            if len(shared_non_generic) >= 1:
                has_legitimate_match = True
            elif len(shared_words) >= 3:
                has_legitimate_match = True
            elif len(q_words) > 0 and (len(shared_words) / len(q_words)) >= 0.6:
                has_legitimate_match = True
            elif has_duration_match:
                has_legitimate_match = True

            if not has_legitimate_match:
                score -= 500.0  # Apply severe penalty to prevent mismatch

    # 7. Duration match scoring (Bonus only)
    if target_duration is not None and scene.get("duration"):
        try:
            scene_dur = float(scene["duration"])
            diff = abs(scene_dur - target_duration)
            if diff < 1.0:
                score += 500.0
            elif diff <= 3.0:
                score += 300.0
            elif diff <= 10.0:
                score += 50.0
        except (ValueError, TypeError):
            pass

    return score


# ── TPDB REST API ─────────────────────────────────────────────────────────────
_TPDB_HEADERS = lambda: {
    "Authorization": f"Bearer {THEPORNDB_API_KEY}",
    "Accept": "application/json",
    "User-Agent": "octor-sidecar/2",
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
        r = SESSION.get(url, headers=_TPDB_HEADERS(), timeout=5)
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
        r = SESSION.get(url, headers=_TPDB_HEADERS(), timeout=5)
        r.raise_for_status()
        data = r.json().get("data") or []
        return [_normalise_tpdb(d) for d in data]
    except Exception as e:
        log(f"TPDB raw search failed: {e}")
        return []

def _normalise_tpdb(d: dict, source: str = "tpdb") -> dict:
    """Flatten TPDB scene response to a common dict."""
    site_obj = d.get("site") or {}
    
    # Prioritize cached vertical posters over dead publisher CDNs (like digitaloceanspaces)
    urls = []
    if d.get("poster"):
        urls.append(d["poster"])
    if isinstance(d.get("posters"), dict):
        posters_dict = d["posters"]
        for k in ["poster", "full", "large"]:
            val = posters_dict.get(k)
            if val and val not in urls:
                urls.append(val)
    for f in ["poster_image", "image"]:
        val = d.get(f)
        if val and val not in urls:
            urls.append(val)

    poster = None
    for u in urls:
        if "theporndb.net" in u:
            poster = u
            break
    if not poster and urls:
        for u in urls:
            if "digitaloceanspaces" not in u:
                poster = u
                break
        if not poster:
            poster = urls[0]
            
    site_name = site_obj.get("name") if isinstance(site_obj, dict) else None
    parent_name = None
    network_name = None
    if isinstance(site_obj, dict):
        parent_obj = site_obj.get("parent")
        if isinstance(parent_obj, dict):
            parent_name = parent_obj.get("name")
        network_obj = site_obj.get("network")
        if isinstance(network_obj, dict):
            network_name = network_obj.get("name")
    
    return {
        "id": d.get("_id") or d.get("id") or d.get("slug"),
        "title": d.get("title"),
        "date": d.get("date") or d.get("release_date") or d.get("production_date"),
        "site": site_name,
        "parent": parent_name,
        "network": network_name,
        "description": d.get("description") or d.get("details") or "",
        "performers": d.get("performers") or [],
        "tags": [t.get("name") for t in d.get("tags") or [] if t.get("name")],
        "poster": poster,
        "duration": d.get("duration"),
        "rating": d.get("rating"),
        "url": d.get("url"),
        "_source": source,
    }

# ── StashDB GraphQL ───────────────────────────────────────────────────────────
_STASHDB_QUERY = """
query ($term: String!) {
  searchScene(term: $term) {
    id title details release_date duration
    studio {
      name
      parent {
        name
      }
    }
    performers { performer { name } as }
    tags { name }
    images { url width height }
    urls { url site { name } }
  }
}
"""

def stashdb_search(term: str) -> List[dict]:
    log(f"StashDB search initiated for term: '{term}' (key present: {bool(STASHDB_API_KEY)})")
    if not STASHDB_API_KEY or not term:
        return []
    headers = {"Content-Type": "application/json", "Apikey": STASHDB_API_KEY}
    try:
        r = SESSION.post(
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
        log(f"StashDB returned {len(scenes)} scenes")
        return [_normalise_stashdb(s) for s in scenes]
    except Exception as e:
        log(f"StashDB search failed: {e}")
        return []

def _normalise_stashdb(s: dict) -> dict:
    images = s.get("images") or []
    # StashDB: try to find a vertical image (height > width) avoiding dead digitaloceanspaces URLs
    poster = None
    for img in images:
        u = img.get("url")
        if u and img.get("height", 0) > img.get("width", 0):
            if "digitaloceanspaces" not in u:
                poster = u
                break
    if not poster:
        for img in images:
            u = img.get("url")
            if u and "digitaloceanspaces" not in u:
                poster = u
                break
    if not poster and images:
        poster = images[0].get("url")
    studio = s.get("studio") or {}
    parent_name = None
    if isinstance(studio, dict):
        parent_obj = studio.get("parent")
        if isinstance(parent_obj, dict):
            parent_name = parent_obj.get("name")
    return {
        "id": s.get("id"),
        "title": s.get("title"),
        "date": s.get("release_date"),
        "site": studio.get("name") if isinstance(studio, dict) else None,
        "parent": parent_name,
        "description": s.get("details"),
        "performers": s.get("performers") or [],
        "tags": [t.get("name") for t in s.get("tags") or [] if t.get("name")],
        "poster": poster,
        "duration": s.get("duration"),
        "rating": None,
        "url": None,
        "_source": "stashdb",
    }

# Helper to generate smart fallback search terms for adult content (performer and sub-phrase extracts)
_GENERIC_WORDS = {
    "hardcore", "sex", "exposed", "titties", "stepmom", "stepsister", "caring",
    "sharing", "sharingiscaring", "huge", "exposed", "tits", "fucking", "anal",
    "pussy", "first", "teacher", "stepparent", "stepdad", "stepson"
}

def get_search_terms(name: str) -> List[str]:
    if not name:
        return []
    terms = [name]
    words = name.split()
    if len(words) > 2:
        # First 2 words (typically performer e.g. "Emily Willis")
        first_two = " ".join(words[:2])
        if first_two.lower() not in _GENERIC_WORDS:
            terms.append(first_two)
        # Last 2 words (typically performer e.g. "Jade Baker")
        last_two = " ".join(words[-2:])
        # Only add if not fully composed of generic words
        if not all(w.lower() in _GENERIC_WORDS for w in words[-2:]):
            if last_two not in terms:
                terms.append(last_two)
    return [t for t in terms if len(t) > 2]

# ── Multi-strategy NSFW lookup (namer-style passes) ───────────────────────────
def adult_enrichment_lookup(title: str, duration: Optional[float] = None) -> Optional[dict]:
    """
    Namer-style multi-pass search parallelized for speed:
      JAV fast-path: if title looks like a JAV code → TPDB /jav endpoint
      All other passes (site+date, site-only, fallbacks, StashDB) executed in parallel.
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

    search_tasks = []

    # ── Task Set 1: Structural Passes ──────────────────────────────────────────
    if site or date or name:
        search_tasks.append((tpdb_search, (site, date, name)))           # Pass 1: full
    if date:
        search_tasks.append((tpdb_search, (site, None, name)))           # Pass 2: skip date
    if name:
        search_tasks.append((tpdb_search, (site, date, None)))           # Pass 3: skip name
    if site:
        search_tasks.append((tpdb_search, (None, date, name)))           # Pass 4: skip site
    search_tasks.append((tpdb_search, (None, None, name or title)))      # Pass 5: name only

    # ── Task Set 2: Fallbacks & StashDB ────────────────────────────────────────
    clean = name_cleaner(title)
    search_terms = get_search_terms(name or clean)
    performer = parsed.get("performer")
    if performer:
        if performer not in search_terms:
            search_terms.insert(0, performer)
        if site:
            site_perf = f"{site} {performer}"
            if site_perf not in search_terms:
                search_terms.insert(0, site_perf)

    for term in search_terms:
        # Structured TPDB search if we have a known site
        if site:
            search_tasks.append((tpdb_search, (site, None, term, 25)))
        # Raw TPDB search
        search_tasks.append((tpdb_search_raw_q, (term, 25)))
        # StashDB search
        search_tasks.append((stashdb_search, (term,)))

    log(f"Executing {len(search_tasks)} search tasks in parallel...")
    all_candidates: List[dict] = []
    seen_ids: set = set()

    futures = [SEARCH_EXECUTOR.submit(func, *args) for func, args in search_tasks]
    for future in futures:
        try:
            results = future.result()
            if not results:
                continue
            for r in results:
                rid = r.get("id")
                if rid and r.get("_source") == "stashdb":
                    rid = "stashdb:" + str(rid)
                if rid and rid not in seen_ids:
                    seen_ids.add(rid)
                    all_candidates.append(r)
        except Exception as e:
            log(f"Search task failed: {e}")

    # Score and pick the absolute best candidate from the entire unified pool!
    best = _pick_best(parsed, all_candidates, duration=duration)
    if not best:
        log(f"No candidates found for '{title}'")
        return None

    scene, score = best
    log(f"Best match overall: '{scene.get('title')}' score={score:.1f} (source: {scene.get('_source')})")

    if score < MATCH_THRESHOLD:
        log(f"Score {score:.1f} below threshold {MATCH_THRESHOLD} — rejected")
        return None

    return _to_omdb(scene)


def _pick_best(parsed: dict, candidates: List[dict], duration: Optional[float] = None) -> Optional[Tuple[dict, float]]:
    if not candidates:
        return None
    scored = []
    q_name = parsed.get("name") or ""
    for c in candidates:
        score = score_result(parsed, c, target_duration=duration)
        
        # Calculate a tie-breaker score based on overall string similarity of the title
        c_title = c.get("title") or ""
        tie_breaker = fuzzy_score(q_name, c_title)
        if q_name and c_title:
            try:
                tie_breaker += fuzz.ratio(q_name.lower(), c_title.lower()) * 0.1
            except Exception:
                pass
                
        scored.append((c, score, tie_breaker))
        perf_names = []
        for p in c.get("performers") or []:
            name = p.get("name") or (p.get("performer") or {}).get("name")
            if name:
                perf_names.append(name)
        log(f"Candidate: '{c.get('title')}' site='{c.get('site')}' date='{c.get('date')}' duration={c.get('duration')} score={score:.1f} tie_breaker={tie_breaker:.1f} performers={perf_names}")
    scored.sort(key=lambda x: (x[1], x[2]), reverse=True)
    return scored[0][0], scored[0][1]

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


_STASHDB_FIND_SCENE_QUERY = """
query ($id: ID!) {
  findScene(id: $id) {
    id title details release_date duration
    studio {
      name
      parent {
        name
      }
    }
    performers { performer { name } as }
    tags { name }
    images { url width height }
    urls { url site { name } }
  }
}
"""

def stashdb_lookup_by_id(scene_id: str) -> Optional[dict]:
    log(f"StashDB lookup by ID: {scene_id}")
    if not STASHDB_API_KEY or not scene_id:
        return None
    headers = {"Content-Type": "application/json", "Apikey": STASHDB_API_KEY}
    try:
        r = SESSION.post(
            STASHDB_ENDPOINT,
            headers=headers,
            json={"query": _STASHDB_FIND_SCENE_QUERY, "variables": {"id": scene_id}},
            timeout=5,
        )
        r.raise_for_status()
        data = r.json()
        if data.get("errors"):
            log(f"StashDB find errors: {data['errors']}")
            return None
        scene = (data.get("data") or {}).get("findScene")
        if scene:
            return _normalise_stashdb(scene)
    except Exception as e:
        log(f"StashDB ID lookup failed: {e}")
    return None

def tpdb_lookup_by_id(scene_id: str) -> Optional[dict]:
    url = f"{TPDB_BASE}/scenes/{scene_id}"
    log(f"TPDB ID lookup: {url}")
    try:
        r = SESSION.get(url, headers=_TPDB_HEADERS(), timeout=5)
        if r.status_code == 200:
            data = r.json()
            item = data.get("data") if isinstance(data, dict) else None
            if not item and isinstance(data, dict):
                item = data
            if item:
                return _normalise_tpdb(item)
    except Exception as e:
        log(f"TPDB ID lookup failed: {e}")
    return None

def tpdb_jav_lookup_by_id(scene_id: str) -> Optional[dict]:
    url = f"{TPDB_BASE}/jav/{scene_id}"
    log(f"TPDB JAV ID lookup: {url}")
    try:
        r = SESSION.get(url, headers=_TPDB_HEADERS(), timeout=5)
        if r.status_code == 200:
            data = r.json()
            item = data.get("data") if isinstance(data, dict) else None
            if not item and isinstance(data, dict):
                item = data
            if item:
                res = _normalise_tpdb(item)
                res["_source"] = "tpdb_jav"
                return res
    except Exception as e:
        log(f"TPDB JAV ID lookup failed: {e}")
    return None


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
    sidecar_enrichment_enabled: Optional[str] = Query(None),
    porn: Optional[str] = Query(None),
    duration: Optional[float] = Query(None),
):
    if not t and not i:
        return JSONResponse({"Response": "False", "Error": "No title or ID"}, status_code=400)

    settings = get_settings()
    sidecar_enabled = settings.get("sidecar_enrichment_enabled", True)
    if sidecar_enrichment_enabled is not None:
        sidecar_enabled = sidecar_enrichment_enabled.lower() == "true"

    # ── Cache check ─────────────────────────────────────────────────────────────
    cache_key = f"{t}|{i}|{duration}"
    cached = _cache_get(cache_key)
    if cached is not None:
        log(f"Cache hit for {cache_key!r}")
        return cached

    # ── Native ID Lookup Path ───────────────────────────────────────────────────
    if i:
        normalized_id = i
        if normalized_id.startswith("tpdb="):
            normalized_id = "tpdb:" + normalized_id[5:]
        elif normalized_id.startswith("tpdb_jav="):
            normalized_id = "tpdb_jav:" + normalized_id[9:]
        elif normalized_id.startswith("stash="):
            normalized_id = "stash:" + normalized_id[6:]

        if normalized_id.startswith("tpdb:"):
            scene_id = normalized_id[5:]
            result = tpdb_lookup_by_id(scene_id)
            if result:
                resp = _to_omdb(result)
                _cache_set(cache_key, resp)
                return resp
        elif normalized_id.startswith("tpdb_jav:"):
            scene_id = normalized_id[9:]
            if scene_id.startswith("fallback_"):
                clean_code = scene_id[9:]
                result = tpdb_jav_lookup(clean_code)
            else:
                result = tpdb_jav_lookup_by_id(scene_id)
                if not result:
                    result = tpdb_jav_lookup(scene_id)
            if result:
                resp = _to_omdb(result)
                _cache_set(cache_key, resp)
                return resp
        elif normalized_id.startswith("stash:"):
            scene_id = normalized_id[6:]
            result = stashdb_lookup_by_id(scene_id)
            if result:
                resp = _to_omdb(result)
                _cache_set(cache_key, resp)
                return resp

    # ── Path 1: JAV code ────────────────────────────────────────────────────────
    jav_code = extract_jav_code(t) if t else None
    if jav_code:
        log(f"JAV fast-path for code: {jav_code}")
        result = tpdb_jav_lookup(jav_code)
        if result:
            resp = _to_omdb(result)
            _cache_set(cache_key, resp)
            return resp
        return JSONResponse({"Response": "False", "Error": "JAV not found"}, status_code=404)

    # ── Path 2: Western adult scene ─────────────────────────────────────────────
    # Only search when a known studio name OR structural date pattern is present,
    # or if the query is explicitly flagged as adult content by the upstream parser.
    is_adult = (porn and porn.lower() == "true") or (t and is_adult_content(t))
    if t and is_adult:
        log(f"Adult content detected, attempting enrichment for: {t}")
        data = adult_enrichment_lookup(t, duration=duration)
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
def update_settings(sidecar_enrichment_enabled: bool = Form(...)):
    settings = {"sidecar_enrichment_enabled": sidecar_enrichment_enabled}
    try:
        save_settings(settings)
    except Exception as e:
        log(f"Error saving settings: {e}")
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)
    return {"status": "ok", "sidecar_enrichment_enabled": sidecar_enrichment_enabled}
