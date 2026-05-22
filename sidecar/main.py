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
SIDECAR_ENRICHMENT_ENABLED = os.getenv("SIDECAR_ENRICHMENT_ENABLED", "true").lower() != "false"

SETTINGS_FILE = Path(os.getenv("SIDECAR_DATA_DIR", "./data") + "/settings.json")

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
    return {"sidecar_enrichment_enabled": SIDECAR_ENRICHMENT_ENABLED}

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

# Date and site parsers are defined after _NSFW_STUDIOS

# ── NSFW studio detection ─────────────────────────────────────────────────────
# Normalised (lowercase, no separators) studio names that unambiguously
# indicate adult content when found in a torrent title.
# Rule: only include names that CANNOT appear in mainstream movie/show titles.
_NSFW_STUDIOS = {
    "18eighteen", "2chickssametime", "40somethingmag", "50plusmilfs", "60plusmilfs", "8thstreetlatinas", "adamevepictures", "adulttime",
    "alsscan", "americandaydreams", "amourbabes", "analintroductions", "analmom", "assparade", "babes", "babygotboobs", "badoinkvr", "bellesafilms",
    "bamvisions", "bangbros", "bangbus", "bangcasting", "bangpov", "bffs", "bifuckkink", "bigbuttslikeitbig",
    "bignaturals", "bigtitsatschool", "bigtitsatwork", "bigtitsboss", "bigtitsinsports", "bigtitsinuniform", "bigtitsroundasses", "bigwetbutts",
    "bikebabes", "bikinicrashers", "bizarrevideotranssexual", "blackandstacked", "blacked", "blackedraw", "blackisbetter", "blackmailed",
    "blackonblondes", "blacksonblondes", "blacksoncougars", "blacksonsluts", "blackstepdad", "blackvalleygirls", "blackwhitefuckfest", "bleufilms",
    "bloodangels", "blowjobfridays", "blowjobninjas", "bluecollarbabes", "bluefantasies", "bodyinmind", "bondagecompound", "bonedathome",
    "bonusholeboys", "boobsquad", "bootyfullbabes", "bootyliciousmag", "boppingbabes", "borderpatrolsex", "bossymilfs", "boundgagged",
    "boundgangbangs", "boundgods", "boundinpublic", "bountyhunterporn", "bracefaced", "brandibellecom", "brattamer", "brattymilf", "brattybabesownyou",
    "brattybarebabes", "brattysis", "brazzers", "brazzersexxtra", "brazzersinteractive", "brazzersplus", "brazzersvip", "brazzersvr", "breedingmaterial",
    "britishbukkakebabes", "brokenbabes", "brownbunnies", "bubblegumdungeon", "burningangel", "busstop", "bustedbabysitters", "bustyadventures",
    "bustyangelique", "bustyarianna", "bustycollegecoeds", "bustydanniashe", "bustydustystash", "bustyinescudna", "bustykellykay", "bustykerrymarie",
    "bustylornamorgan", "bustymerilyn", "bustyoldsluts", "bustyones", "bustysammieblack", "buttdivers", "buttmachineboys", "buttman",
    "candymonroe", "canhescore", "captivemale", "cardiogasm", "caseyatruestory", "casting", "caughtfapping", "caughtmycoach",
    "cfnm", "cfnmshow", "cfnmteens", "chantasbitches", "charlesdera", "charmed", "chasewaterbabes", "chastitybabes",
    "cheatinghotbabes", "cheatingsis", "cheatingwithmyex", "cherrybrady", "cherrypop", "chloesworld", "chongas", "chrisstreamsproductions", "clubsweethearts",
    "christophsbignaturaltits", "christymarks", "ciaobella", "cinematickink", "classroom", "clinicaltorments", "cock4stepmom", "collegebabesexposed",
    "collegebash", "collegerules", "colombiafuckfest", "cosplaybabes", "cosplayground", "cougarseductions", "coupleswapping", "courtneytaylor", "creampieforgranny",
    "creampiefunbabes", "cristalkinky", "cruelmedia", "crystalgunnsworld", "cuckoldsessions", "cum4k", "cumfiesta", "cumswappingsis", "cutiepie",
    "czechcasting", "czechstreets", "czechvr", "czechvrnetwork", "dadcrush", "daddygetslucky", "daddypounds", "daddysboy",
    "daddysgirl", "daddyslilangel", "dadsloveporn", "daisytaylor", "daughterswap", "davidperry", "daylenerio", "ddfbabes",
    "deeper", "desiraesworld", "desireedulce", "detentiongirls", "devilsfilm", "devicebondage", "dfxbigbangz", "dfxsolemates", "dfxtracompilations",
    "dfxtraoriginals", "dianepoppos", "diaryofamilf", "diaryofananny", "digitalplayground", "digitalsin", "dilfed", "dirtcheapteens",
    "dirtymasseur", "dirtyworldtour", "divinebitches", "doctoradventures", "dogfartnetwork", "doghousedigital", "domai", "dontbreakme", "dorcel",
    "dorminvasion", "driverxxx", "dronehunter", "dungeonsex", "dyked", "ebonythots", "edgedandbound", "electrosluts",
    "elegantanal", "elegantangel", "emilywillis", "eroticababes", "eroticbeauty", "eternaldesire", "eurobabeschannel", "eurofoxes",
    "evanottyvideos", "everythingbutt", "evilangel", "evilshow", "exotic4k", "extras", "extremepickups", "exxxtrasmall", "facialfest",
    "familiestied", "familysinners", "familystrokes", "familystrokesfeatures", "familyswap", "familyxxx", "fans", "farthammer",
    "feedherfuckher", "feedme", "femalesubmission", "fetishmodelpupett", "filthsyndicate", "filthyfamily", "firstclasspov", "firsttimefootsmellers", "fitnessrooms",
    "flatandfuckedmilfs", "footjobfantasiescumtrue", "footsiebabes", "footworship", "forbiddenseductions", "fostertapes", "freaksinside", "freakyfembots",
    "freeusefantasy", "freeusesingles", "fuckedandbound", "fuckfordollars", "fuckstudies", "fuckingmachines", "fuckmyass", "fuckteamfive", "futasentaisquad",
    "futaworld", "futuredarkly", "gayrevenge", "gfflive", "gfforiginals", "ginarysgiantessadventures", "ginaryskinkyadventures", "ginarystickleadventures",
    "gingerpatch", "girlcore", "girlfriendsfilms", "girlgirl", "girlgirlxxx", "girlsunderarrest", "girlswholie", "gloryholeloads",
    "glowupz", "goddessginary", "goddessnudes", "greedy", "grinders", "hardcoregangbang", "hardkinks", "harmonyfetish",
    "hazeher", "hentaisexschool", "herfreshmanyear", "heteroflexible", "hiddenlayers", "hijabhookup", "hkjfans", "hogtied",
    "homealonemilfs", "honeybabesugarpiie", "hornybirds", "hotbabes4k", "hotkinkyjoxxx", "hotkinkyme", "housewife1on1", "howmenorgasm",
    "hustler", "hustlerparodies", "hypkinkseductionispower", "iconfessfiles", "iconmale", "ifilmmyself", "ihaveawife", "iknowthatgirl",
    "iloveblackshemales", "imadeporn", "indianbabes", "innocenthigh", "insidenaughtyamerica", "internalviolations", "intimatelypov", "isthisreal",
    "iwantmysister", "jadekink", "jaytaylorxx", "jizzonmyjugs", "johnbloomberg", "joibabes", "joimom", "jonnidarkkoxxx",
    "jordi", "joymii", "juicypinkbox", "julesjordan", "julesjordannetwork", "karinahart", "karupsprivatecollection", "katsokinky",
    "kellymadison", "kellymadisonmedia", "kevinmoore", "kgbthetravelcamstudio", "kingmidas", "kink", "kink305", "kinkacademy",
    "kinkbomb", "kinkcanary", "kinkclassics", "kinkcompilations", "kinkdevice", "kinkfeatures", "kinkicory", "kinklive",
    "kinkmen", "kinkmenclassics", "kinkmenseries", "kinkmentestshoots", "kinkoncommand", "kinkrawtestshoots", "kinksisters", "kinktestshoots",
    "kinktrans", "kinkuniversity", "kinkxbizarrevideo", "kinkxdigitalsin", "kinkxteaseandthankyou", "kinkyangels", "kinkybites", "kinkybitesmen",
    "kinkydogs", "kinkyexploits", "kinkyfamily", "kinkyfeetures", "kinkygirlsberlin", "kinkyinlaws", "kinkykingdom", "kinkyleatherclips",
    "kinkymarisol", "kinkymistresses", "kinkypanthersbdsm", "kinkypleasures", "kinkyponygirl", "kinkyrubberworld", "kinkysex", "kinkysluts4k",
    "kinkyspa", "kinkytwink", "kinkyvisions", "kissingsis", "lacylennon", "ladygonzo", "lasluts", "latexplaytime", "latinamilf", "legalporno", "lifeselector",
    "latinachannel", "latinarampage", "latinasextapes", "latinastepmom", "latinateam", "legsex", "lesbiancrimestories", "lesbianghoststories",
    "lesbiangirlongirl", "letsbebad", "letspostit", "letstryanal", "lild", "lillatinas", "lilsis", "linseysworld", "lukecooperx",
    "littleasians", "livenaughtystudent", "livingwithanna", "lovehairy", "lunastar", "lusthd", "magicalfeet", "mandyiskinky",
    "marinavaylor", "mature4k", "meanbitch", "meninpain", "menonedge", "messyjessy", "metart", "metartx", "metmodels",
    "miakhalifa", "michaelninn", "mickybells", "mikesapartment", "milehighmedia", "milehighxtreme", "milfed", "milfhunter",
    "milflessons", "milfslikeitbig", "milfslikeitblack", "milfsoup", "milfsugarbabesclassic", "milftugs", "milfy", "milfty", "milkenema",
    "milkybabes", "millymarks", "mixedx", "mofos", "momcomesfirst", "momisamilf", "momishorny", "momknowsbest", "mommygotboobs",
    "mommysboy", "mommystoytime", "momsanaladventure", "momsbangteens", "momsincontrol", "momslickteens", "momdrips", "mommy4k", "momsmoney", "momsteachsex", "momwantscreampie", "momwantstobreed",
    "mondofetiche", "moneytalks", "monstercurves", "monstersofcock", "mormongirlz", "motherdaughterexchangeclub", "mranal", "mrcameltoe",
    "mybabysittersclub", "mydirtymaid", "mydirtyuncle", "mydirtyvault", "myfamilypies", "myfirst", "myfirstsexteacher", "myfriendsfeet",
    "myfriendshotgirl", "myfriendshotmom", "mygf", "mykinkydope", "mylf", "mylfxevilangel", "mylfxteamskeet", "mylifeinbrazil", "mypornbabes",
    "nakedhustlers", "nakedkombat", "nakedyogalife", "naughtyamerica", "naughtyamericanetwork", "naughtyamericans", "naughtyamericavr", "naughtyathletics",
    "naughtybookworms", "naughtycountrygirls", "naughtyflipside", "naughtymag", "naughtyoffice", "naughtyrichgirls", "naughtystaff", "naughtyweddings",
    "newbieblack", "nfbusty", "newsensations", "newsensationsgirlsway", "newsensationsxanalized", "newsensationsxarchangel", "newsensationsxbang", "newsensationsxslutinspection", "newsensationsxwifebucket",
    "nikkijadetaylor", "nofaces", "noirmale", "notmygrandpa", "nubilescasting", "nubileset", "nubilesnet", "nubilesporn",
    "nubilespornnetwork", "nubilesunscripted", "officeobsession", "oldhornymilfs", "onlyfans", "onlytarts", "oopsfamily", "oopsie", "oopsieanimated",
    "openfamily", "ourlittlesecret", "oyeloca", "pansexualx", "papaloads", "partygirls", "partyof3", "passportbros",
    "pawg", "pegging", "pennyshow", "penthouse", "penthousegold", "perfectfuckingstrangersclassic", "perspective", "pervdoctor",
    "pervdriver", "pervmassage", "pervmom", "pervtherapy", "pervzfeatures", "pervzsingles", "petiteballerinasfucked", "petitehdporn",
    "petiteteens18", "pickinguppussy", "pinko", "pissing", "polyfamilylife", "pornfidelity", "pornmegaload", "pornstarwife", "pornstarplatinumkink",
    "pornstarslikeitbig", "pornstarspa", "pornstarspunishment", "pornstarvote", "pov4k", "povfantasy", "povlife", "povmassage",
    "povmasters", "povpickups", "powermunch", "preggoworld", "prettydirty", "princesscum", "private", "privateblack",
    "privatecastings", "privatecastingx", "privateclassics", "privatefetish", "privategwen", "privatejet", "privateman", "privatemilfs",
    "privateplace", "privatepornvideo", "privateschooljewel", "privatesextapes", "privatesociety", "privatestars", "privateteenvideo", "privatetranssexual",
    "projectdtf", "projectrv", "propertysex", "publicagent", "publicbang", "publicdisgrace", "publicinvasion", "publicpickups",
    "punishteens", "pure18", "puretaboo", "purets", "pussypatrol", "pvcbabes", "queensofkink", "rachelstarr",
    "randysroadstop", "realbutts", "realfuckingcouples", "realgirlsnow", "realityjunkies", "realitykings", "realitysis", "realpornstarsvr",
    "realwifestories", "reneerossvideo", "reptyleclassics", "reptylefeatures", "reptylelabs", "reptyleselects", "rkprime", "rkshorts",
    "roccosiffredi", "roundandbrown", "rubateen", "russianfakeagent", "rustytaylor", "sadiewest", "sadisticrope", "sarataylor",
    "sarennasworld", "savagegangbang", "sayunclexpakinky", "scalebustinbabes", "scoreclassics", "scorehd", "scoreland", "scoreland2",
    "scoretheater", "scorevideos", "secretcrush", "seducedbyacougar", "selfdesire", "sexandgrades", "sexandsubmission", "sexart",
    "sexbusters", "sexmex", "sexselector", "sexybabes", "sexyclubbabes", "sexysandeecom", "shanedieselsbanginbabes", "shapeofbeauty", "sharizelvideos",
    "shesbreedingmaterial", "shesnew", "shewantshim", "shoplyfter", "showersolos", "showmybf", "singlemoms", "sislovesme",
    "sisswap", "sisters", "sistertrick", "slayed", "sleazystepdad", "slutstepmom", "slutstepsister", "sluttywhitegirls",
    "smashed", "sneakysex", "sofiarose", "solointerviews", "sophiedee", "spanish18", "spankmonster", "spizoo", "spermglazed", "sportbabes",
    "springbreak", "stacyvandenbergboobs", "stayhomepov", "stepfamilychannel", "stepmomlessons", "stepmomvideos", "stepsiblings", "stepsiblingscaught",
    "straplez", "streetranger", "strugglingbabes", "stunning18", "submissived", "sugarbabestv", "summervacation", "supersluts",
    "swappzfeatures", "swappzsingles", "sweetheartvideo", "sweetsinner", "sweetsweetsallymae", "switch", "sxoriginals", "takenrough",
    "tawnypeaks", "taylorlittle", "taylormadeclips", "taylorraz", "taylorsfetishemporium", "taylortitfucks", "taylortwins", "taylorwaneentertainment",
    "teacherfucksteens", "teamskeet", "teamskeetallstars", "teamskeetclassics", "teamskeetextras", "teamskeetfeatures", "teamskeetlabs", "teamskeetnetwork", "teamskeetselects",
    "teamskeetsingles", "teamskeetvip", "5kporn", "teamskeetxadultprime", "teamskeetxamazingfilms", "teamskeetxaussiefellatioqueens", "teamskeetxaveryblack", "teamskeetxbaeb",
    "teamskeetxbananafever", "teamskeetxbang", "teamskeetxbjraw", "teamskeetxbrandibraids", "teamskeetxbrasilbimbos", "teamskeetxbrattyfootgirls", "teamskeetxbritstudioxxx", "teamskeetxcamsoda",
    "teamskeetxcannonproductions", "teamskeetxclubcastings", "teamskeetxclubsweethearts", "teamskeetxcumkitchen", "teamskeetxdantecolle", "teamskeetxdoctaytay", "teamskeetxerotiquetvlive", "teamskeetxevaelfie",
    "teamskeetxevilangel", "teamskeetxfamilyscrew", "teamskeetxfilthykings", "teamskeetxfit18", "teamskeetxflorarodgers", "teamskeetxfuckingawesome", "teamskeetxfuckingskinny", "teamskeetxgotfilled",
    "teamskeetxgranddadz", "teamskeetxharmonyfilms", "teamskeetxherbcollins", "teamskeetxhobybuchanon", "teamskeetxhussiepass", "teamskeetximmaybee", "teamskeetximpuredesire", "teamskeetxjamesdeen",
    "teamskeetxjapornxxx", "teamskeetxjasonmoody", "teamskeetxjavhub", "teamskeetxjonathanjordan", "teamskeetxjoybear", "teamskeetxkatekoss", "teamskeetxkrisskiss", "teamskeetxlaynalandry",
    "teamskeetxlethalhardcore", "teamskeetxlunaxjames", "teamskeetxluxurygirl", "teamskeetxmanko88", "teamskeetxmickeymod", "teamskeetxmodelmediaasia", "teamskeetxmodelmediaus", "teamskeetxmollyredwolf",
    "teamskeetxmybestsexlife", "teamskeetxmymilfz", "teamskeetxog", "teamskeetxonly3x", "teamskeetxpornfidelity", "teamskeetxpurgatoryx", "teamskeetxrawattack", "teamskeetxreislin",
    "teamskeetxrileycyriis", "teamskeetxscreampies", "teamskeetxsheseducedme", "teamskeetxsinematica", "teamskeetxspankmonster", "teamskeetxsparksentertainment", "teamskeetxspizoo",
    "teamskeetxstellasedona", "teamskeetxstephousehold", "teamskeetxsweetiefox", "teamskeetxtabbyandnoname", "teamskeetxtenshigao", "teamskeetxthicc18", "teamskeetxtoughlovex", "teamskeetxwilltilexxx",
    "teamskeetxxanderporn", "teamskeetxxxxjobinterviews", "teamskeetxyesgirlz", "teamskeetxyoungbusty", "teenagelesbian", "teencurves", "teenfidelity", "teenjoi",
    "teenoverload", "teenpies", "teenpinkvideos", "teensatwork", "teensdoporn", "teenslikeitbig", "teenslikeitblack", "teensloveanal",
    "teensloveblackcocks", "teenslovecream", "teenslovehugecocks", "teenslovemoney", "teenyblack", "teslataylor", "theadulttimepodcast", "thebrats",
    "thefootinfatuation", "thekinkfaeriex", "thelifeerotic", "theloft", "themikeandjoannashow", "theminion", "thepassenger", "therealworkout",
    "thescoregroup", "thesexscout", "thespa", "thetrainingofo", "theupperfloor", "theyeslist", "thickumz", "thisgirlsucks",
    "thundercock", "tickleaddiction", "tiffanytaylor", "tiffanytowers", "tinysis", "titsandtugs", "tittyattack", "tomboyish",
    "tomboyz", "tonightsfuck", "tonightsgirlfriend", "tonightsts", "tonniataylor", "toywithme", "trannysurprise", "trannytemptation", "transfixed",
    "transgressivefilms", "transsensual", "transsexualangel", "trophywives", "truelesbian", "truesexstories", "tsdivas", "tsfactor",
    "tskink", "tsplayground", "tspussyhunters", "tsseduction", "tugjobs", "turningtwistys", "tushy", "tushyraw",
    "twistys", "twistyshard", "twistysteasers", "uksoccerbabes", "ultimatesurrender", "underthebed", "unrelatedx", "upclosex",
    "valoryirene", "vengeancexxx", "virtualporn", "virtualtaboo", "viviantaylor", "vivid", "vividalt", "vividceleb",
    "vividclassic", "vivthomas", "vixen", "vixenmediagroup", "vrcosplayx", "wasteland", "watchingmydaughtergoblack", "watchingmymomgoblack",
    "watchyourmom", "waterbondage", "wefuckblackgirls", "welivetogether", "wetforwomen", "whengirlsplay", "wheretheboysarent", "whippedass",
    "wifewriting", "wifey", "wiredpussy", "withlovelexi", "wivesonvacation", "wolfwagner", "womenseekingwomen", "womensworld", "woodmanentertainment",
    "workinglatinas", "wrestlingmale", "xandercorvus", "xlgirls", "xxxpawn", "youngandcurious", "youngermommy", "youngkink",
    "yourwifemymeat", "zebragirls",
    "onlyfans", "8kmilfs", "bbcsurprise", "bigcockbully", "bigcockhero", "bigmouthfuls",
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
    Parse an adult torrent name into {site, date, name}.
    """
    stem = re.sub(r'\.(mp4|mkv|avi|mov|wmv|webm|ts)$', '', title, flags=re.I)
    
    result = {"site": None, "date": None, "name": None, "raw": title}

    # 1. Try to extract date
    date_info = extract_date(stem)
    if date_info:
        date_str, d_start, d_end = date_info
        result["date"] = date_str
        stem = stem[:d_start] + " " + stem[d_end:]
    
    # 2. Try to extract known studio
    studio_info = extract_studio(stem)
    if studio_info:
        studio_key, orig_site = studio_info
        result["site"] = orig_site.strip(" -._")
        stem = stem.replace(orig_site, " ")

    # 3. Clean up remaining stem for scene name
    cleaned_name = name_cleaner(stem)
    cleaned_name = re.sub(r'^[-\s._()]+', '', cleaned_name)
    cleaned_name = re.sub(r'[-\s._()]+$', '', cleaned_name)
    result["name"] = cleaned_name if cleaned_name else None

    log(f"Parsed filename → site={result['site']!r} date={result['date']!r} name={result['name']!r}")
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
        # Reject if the prefix is a known western adult studio
        if prefix.lower() in _NSFW_STUDIOS:
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
        # Prefer vertical poster if explicitly listed in posters dict
        poster = d["posters"].get("poster") or d["posters"].get("full") or d["posters"].get("large")
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
      site match       → +100
      date match       → +100
      name fuzzy       → 0-100
      performer boost  → +150 (if a performer name is in the filename)
    """
    score = 0.0

    # Site match / mismatch penalty (including parent/network sites)
    if parsed.get("site"):
        parsed_site = re.sub(r'[^a-z0-9]', '', unidecode(parsed["site"]).lower())
        
        # Check direct site match first
        scene_site = re.sub(r'[^a-z0-9]', '', unidecode(scene.get("site") or "").lower())
        if parsed_site and scene_site and (parsed_site in scene_site or scene_site in parsed_site):
            score += 120  # Direct site match gets a premium boost
        else:
            # Check parent/network matches
            scene_sites = []
            if scene.get("parent"):
                scene_sites.append(re.sub(r'[^a-z0-9]', '', unidecode(scene["parent"]).lower()))
            if scene.get("network"):
                scene_sites.append(re.sub(r'[^a-z0-9]', '', unidecode(scene["network"]).lower()))
                
            matched = False
            for s_name in scene_sites:
                if s_name and (parsed_site in s_name or s_name in parsed_site):
                    matched = True
                    break
                    
            if matched:
                score += 100  # Network/parent match fallback
            else:
                score -= 150  # Explicit site mismatch penalty
    else:
        # No site info in filename — don't penalise
        score += 50

    # Date match
    if parsed.get("date") and scene.get("date"):
        if parsed["date"] == scene["date"][:10]:
            score += 100
    elif not parsed.get("date"):
        score += 50  # no date info — don't penalise

    # Performer match boost
    perf_match = False
    raw_title = parsed.get("raw", "").lower()
    for p in scene.get("performers") or []:
        name = p.get("name") or (p.get("performer") or {}).get("name")
        if name:
            name_lower = name.lower()
            if name_lower in raw_title:
                perf_match = True
                break
            cleaned_perf = re.sub(r'[^a-z0-9]', '', name_lower)
            cleaned_raw = re.sub(r'[^a-z0-9]', '', raw_title)
            if cleaned_perf in cleaned_raw:
                perf_match = True
                break

    if perf_match:
        score += 150

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
    
    # TPDB: Try to prioritize vertical posters if possible
    # Note: TPDB standard API 'poster' field is usually the vertical one.
    # 'image' or 'poster_image' might be horizontal backdrops.
    poster = d.get("poster") or d.get("poster_image") or d.get("image")
    
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
        "description": d.get("description") or d.get("details"),
        "performers": d.get("performers") or [],
        "tags": [t.get("name") for t in d.get("tags") or [] if t.get("name")],
        "poster": poster,
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
    log(f"StashDB search initiated for term: '{term}' (key present: {bool(STASHDB_API_KEY)})")
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
        log(f"StashDB returned {len(scenes)} scenes")
        return [_normalise_stashdb(s) for s in scenes]
    except Exception as e:
        log(f"StashDB search failed: {e}")
        return []

def _normalise_stashdb(s: dict) -> dict:
    images = s.get("images") or []
    # StashDB: try to find a vertical image (height > width)
    poster = None
    for img in images:
        if img.get("height", 0) > img.get("width", 0):
            poster = img.get("url")
            break
    if not poster and images:
        poster = images[0].get("url")
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
def adult_enrichment_lookup(title: str) -> Optional[dict]:
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

    # If we already have a perfect high-confidence match (site + date + performer), we can stop early
    best = _pick_best(parsed, all_candidates)
    if best and best[1] >= 280:
        log(f"Perfect structured match found: '{best[0].get('title')}' score={best[1]:.1f}")
        return _to_omdb(best[0])

    # Pass 6: Fallback searches with smart terms (TPDB raw, TPDB structured by site, StashDB)
    clean = name_cleaner(title)
    search_terms = get_search_terms(name or clean)
    for term in search_terms:
        # Structured TPDB search if we have a known site
        if site:
            log(f"TPDB structured fallback for term: '{term}' on site '{site}'")
            site_results = tpdb_search(site, None, term, limit=25)
            for r in site_results:
                rid = r.get("id")
                if rid and rid not in seen_ids:
                    seen_ids.add(rid)
                    all_candidates.append(r)

        # Raw TPDB search fallback
        log(f"TPDB raw search fallback for term: '{term}'")
        raw_results = tpdb_search_raw_q(term, limit=25)
        for r in raw_results:
            rid = r.get("id")
            if rid and rid not in seen_ids:
                seen_ids.add(rid)
                all_candidates.append(r)

        # StashDB fallback
        stash_results = stashdb_search(term)
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
    scored = []
    for c in candidates:
        score = score_result(parsed, c)
        scored.append((c, score))
        perf_names = []
        for p in c.get("performers") or []:
            name = p.get("name") or (p.get("performer") or {}).get("name")
            if name:
                perf_names.append(name)
        log(f"Candidate: '{c.get('title')}' site='{c.get('site')}' date='{c.get('date')}' score={score:.1f} performers={perf_names}")
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
    sidecar_enrichment_enabled: Optional[str] = Query(None),
):
    if not t and not i:
        return JSONResponse({"Response": "False", "Error": "No title or ID"}, status_code=400)

    settings = get_settings()
    sidecar_enabled = settings.get("sidecar_enrichment_enabled", True)
    if sidecar_enrichment_enabled is not None:
        sidecar_enabled = sidecar_enrichment_enabled.lower() == "true"

    # ── Cache check ─────────────────────────────────────────────────────────────
    cache_key = f"{t}|{i}"
    cached = _cache_get(cache_key)
    if cached is not None:
        log(f"Cache hit for {cache_key!r}")
        return cached

    # ── Path 1: JAV code ────────────────────────────────────────────────────────
    jav_code = extract_jav_code(t) if t else None
    if jav_code and sidecar_enabled:
        log(f"JAV fast-path for code: {jav_code}")
        result = tpdb_jav_lookup(jav_code)
        if result:
            resp = _to_omdb(result)
            _cache_set(cache_key, resp)
            return resp
        return JSONResponse({"Response": "False", "Error": "JAV not found"}, status_code=404)

    # ── Path 2: Western adult scene ─────────────────────────────────────────────
    # Only search when a known studio name OR structural date pattern is present.
    if t and sidecar_enabled and is_adult_content(t):
        log(f"Adult content detected, attempting enrichment for: {t}")
        data = adult_enrichment_lookup(t)
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
