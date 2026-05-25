import os
import sys

# Configure environment variables before importing main
os.environ["THEPORNDB_API_KEY"] = "4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf"
os.environ["STASHDB_API_KEY"] = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY"

import main

# Force logging to stdout
def custom_log(msg):
    print(f"  [LOG] {msg}")
main.log = custom_log

# ── All torrent names from stress-test images ─────────────────────────────────
# Image 1: rq.mp4 style (site - performer - title (date))
# Image 2: dot-separated WRB/XC style
# Image 3: more rq.mp4 style
# Image 4: dot-separated HEVC/PRT style

test_titles = [
    # ── Image 1 (rq.mp4 style) ────────────────────────────────────────────────
    "MyPervyFamily - Sienna Santana - Stepsis Surprise In The Shower (21.05.2026) rq.mp4",
    "FamilyTherapyXXX - Elly Clutch - Alone With Sister's Best Friend (21.05.2026) rq.mp4",
    "MomSwap - Krystal Sparks, Jessica Aaren (21.05.2026) rq.mp4",
    "BrownBunnies - Russian Creamx - Big Titty Ebony Loves To Fuck (21.05.2026) rq.mp4",
    "FamilyStrokes - Gal Ritchie (21.05.2026) rq.mp4",
    "MyFamilyPies - Della Cate - My Pussy Isnt Going To Fuck Itself Stepbro (21.05.2026) rq.mp4",
    "HornyHostel - Skylar Snow, Chanel Camryn, Alexa Chains - Office Brats (21.05.2026) rq.mp4",
    "Deeper - Sky Wonderland - Keep It Down (21.05.2026) rq.mp4",
    "OnlyFans - Sarah Henderson - Hotel Room Fucking With A View rq.mp4",
    "BlackedRaw - Jewel Diamant - Top-Shelf Babe Takes A Massive BBC (21.05.2026) rq.mp4",
    "LegalPorno - Kenzie Reeves (10.05.2026) rq.mp4",
    "OnlyFans - Vivienne Vo - Cute Starbucks Girl Gets Fucked During Her Shift In My rq.mp4",
    "OnlyFans - Kate Dalia - Bored Step Sister Wants To Fuck 1080p rq.mp4",
    "Brazzers - Jasmine Sherni - Operation Open Wide (21.05.2026) rq.mp4",
    "OnlyFans - Ruby Rose - Bathroom Sextape rq.mp4",
    "SexMex - Barbielu - Her First Porn Scene (21.05.2026) rq.mp4",
    "HotMilfsFuck - Koda Monroe - I Officially Love Anal (14.09.2025) rq.mp4",
    "OnlyFans - Nadia Ali - Fucked Hard By A Strangers BBC rq.mp4",
    "OopsFamily - Penny Barber - Welcome To Stepmom Airlines (22.05.2026) rq.mp4",
    "School.Bus.Girls.7.2008",
    "YourMomLovesAnal - Jenn Cameron - 43 year old Gets Her Ass Fucked rq.mp4",
    "BrattySis - Ali Jones - Can I Sit On Your Face Stepbro (22.05.2026) rq.mp4",
    "ManyVids - Sweetie Fox - Fem Gyro Zeppeli Fucks Johnny and Gets Cum In Her Mouth rq.mp4",
    "Anal-Angels - Lucy Knight - Do you like my tail (21.05.2026) rq.mp4",
    "The Bachelorette Party The Brides Bodyguard [Life Selector 2025] XXX WEB-DL MP4",
    "OnlyFans - Cami Strella - She took it like a champ 1080p rq.mp4",
    "HotMilfsFuck - Indra - Part Of Romance Is Anal (12.04.2026) rq.mp4",

    # ── Image 2 (dot-separated, WRB/XvX style) ────────────────────────────────
    "OnlyFans 2024 Lily Phillips XXX 1080p MP4-P2P [XC]",
    "MomsFamilySecrets 23 07 27 Jessica Ryan Keeping Stepmom Happy XXX 1080p MP4-WRB [XC]",
    "TeamSkeetXReislin.21.01.19.Reislin.Money.Opens.Every.Door.XXX.1080p.MP4-WRB[XvX]",
    "NewSensations.23.06.06.Octavia.Red.XXX.1080p.MP4-WRB[XvX]",
    "MyFriendsHotMom.20.03.30.Elle.Cee.REMASTERED.XXX.1080p.MP4-LEWD[XvX]",
    "My Wife And Her Black Lover 7 [New Sensations 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "The Ultimate Hotwife Experience Vol. 2 [WIFEY 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "Thr3e 10 [DORCEL 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "Pound Me Daddy 2 [Crave Media 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "MILFs Do It Better [New Sensations 2025] XXX WEB-DL 1080p MP4-P2P [XC]",
    "Anal Desires 5 [Deep Lush 2025] XXX WEB-DL 1080p MP4-P2P [XC]",
    "HardX.24.04.27.Melody.Marks.XXX.1080p.MP4-WRB[XvX]",
    "MatureNL 23 11 24 Nadine And Sharon Amore Threesome Massage XXX 1080p MP4-P2P [XC]",
    "London Keyes Is Yummy [Alex Romero 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "Redheads Vol. 2 [JaysPov 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "BangBus.23.06.07.Leo.Babe.XXX.1080p.MP4-WRB[XvX]",
    "OnlyFans 26 03 13 Gattouz0 And Audrey&Sadie SadieLuvsAudrey A Lucky Tunisian Man With Two Famous Les rq.mp4",
    "Vixen 26 03 22 Emiri Momota In Vogue The Comeback XXX 1080p MP4-P2P [XC]",
    "Ready For My Facial 2 [Reality Kings 2026] XXX WEB-DL 1080p MP4-P2P [XC]",
    "Cum From Behind 11 [PornPros 2026] XXX WEB-DL 1080p MP4-P2P [XC]",

    # ── Image 3 (more rq.mp4 style) ────────────────────────────────────────────
    "HotMilfsFuck - Koda Monroe, Ruby Moon - Taking It To Another Level (19.10.2025) rq.mp4",
    "FamilyTherapyXXX - Elly Clutch - Alone With Sister's Best Friend (21.05.2026) 1080p rq.mp4",
    "School.Bus.Girls.1.2002",
    "OnlyFans - Frances Bentley - One Of My Fuckboys Was So Tense, I Just Had To Help rq.mp4",
    "EnjoyX - Rossa Vaxx - Deep Massage Session (20.02.2026) rq.mp4",
    "OnlyFans - Madiiitay - Sucking Him Off Before We Go Out For The Night rq.mp4",
    "XVideosRED - Sara Blonde - Hardcore DP Threesome rq.mp4",
    "Brazzers - Freya Mayer - Yoga Queen (22.05.2026) rq.mp4",
    "HotMilfsFuck - Selene Love - Use And Abuse Me (09.11.2026) rq.mp4",
    "OnlyFans - Kianna Dior - Brand New Trainer Creampie rq.mp4",
    "FamilyXXX - Cara Mella - Is A Hard Habit To Break (22.05.2026) rq.mp4",
    "OnlyFans - Hotwife Nurse - My Cuck Brought Me To TribalBBC Hotel To Be Creampied rq.mp4",
    "CzechAnalSex - Rima - Asian Teen Loves Anal rq.mp4",
    "RickysRoom - Martina Smeraldi - Martina Rains Pleasure (21.05.2026) rq.mp4",
    "OnlyFans - Onyx Reign - Caught Jerking Off While My Step Sister Does Yoga rq.mp4",
    "ExploitedCollegeGirls 26 05 07 Livi Blossom 1st 3some Didnt Know What To Expect rq.mp4",
    "Brazzers - Nicole Kitt, Jazmin Black - Girthmasterr Fanclub (22.05.2026) rq.mp4",
    "LegalPorno - Nala Brooks (21.05.2026) rq.mp4",
    "JaxSlayher - Lunita Galactica - Out Of This World Anal (21.05.2026) rq.mp4",
    "FakeTaxi - Sata Jones - Laptop Repair Life Saver (22.05.2026) rq.mp4",
    "MomDrips - Elise London (22.05.2026) rq.mp4",
    "DigitalPlayground - Addison Vodka - In The Bag (21.05.2026) rq.mp4",
    "Milfy 26 05 20 Phoenix Marie And Cory Chase Busty MILFs Phoenix And Cory Fuck rq.mp4",

    # ── Image 4 (dot-separated HEVC/PRT style) ────────────────────────────────
    "Vixen.24.05.31.Eve.Sweet.Vanessa.Alessia.And.Lia.Lin.Hotel.Vixen.Season.2.Episode.7.Bachelorette.Get.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.23.08.05.Kendra.Sunderland.Here.To.Stay.XXX.1080p.HEVC.x265.PRT[XvX]",
    "ExploitedCollegeGirls.24.08.22.Marina.Pleaser.With.Intense.Leg.Shaking.Orgasms.XXX.1080p.HEVC.x265.PRT[XvX]",
    "AnalTherapyXXX.24.03.10.Amari.Anne.The.Perfect.Size.XXX.1080p.MP4-WRB[XvX]",
    "Deeper.24.01.11.Blake.Blossom.Host.XXX.1080p.HEVC.x265.PRT[XvX]",
    "OnlyFans.2023.Anna.Ralphs.Sex.In.Bedroom.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.08.01.Amber.Moore.BBC.Hungry.Blonde.Baddie.Amber.Takes.Charge.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.07.27.Octokuro.Insatiable.Beach.Babe.Octokuro.Gets.DPed.By.Two.BBCs.XXX.1080p.HEVC.x265.PRT[XvX]",
    "LegalPorno.24.04.25.Anais.Hayek.And.Hannah.Hayek.XXX.720p.HEVC.x265.PRT[XvX]",
    "MyPervyFamily.24.03.16.Jessie.Rogers.Lucky.My.Dad.Is.A.Dirtbag.XXX.720p.HEVC.x265.PRT[XvX]",
    "Vixen.24.07.26.Kelly.Collins.And.Stefany.Kyler.New.Obsession.Part.3.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.04.03.Penny.Barber.Naughty.MILF.Penny.Risks.It.All.For.Two.Thick.BBCs.XXX.720p.HEVC.x265.PRT[XvX]",
    "OnlyFans.2023.Eva.Elfie.I.Let.My.Step.Bro.Creampie.My.Pussy.XXX.720p.HEVC.x265.PRT[XvX]",
    "SexArt.24.03.06.Liz.Ocean.Give.Up.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.01.27.Kendra.Sunderland.Size.Queen.Kendra.Needs.A.Real.BBC.To.Please.Her.XXX.1080p.HEVC.x265.PRT[XvX]",
    "BackroomCastingCouch.24.08.12.Juniper.The.Farm.Girl.Experience.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.08.11.Lia.Lin.Temptress.Lia.Has.BBC.Threesome.With.Coworkers.XXX.1080p.HEVC.x265.PRT[XvX]",
    "Blacked.24.05.13.Sarah.Illustrates.Naughty.Wife.Sarah.Sneaks.In.A.Secret.BBC.Workout.XXX.720p.HEVC.x265.PRT[XvX]",
    "OnlyFans.2023.Anna.Ralphs.Pussy.Creampie.PPV.XXX.1080p.HEVC.x265.PRT[XvX]",
    "NewSensations.23.06.06.Octavia.Red.XXX.1080p.HEVC.x265.PRT[XvX]",
    
    # ── Image 5 (mismatch testing / new stress test names) ────────────────────
    "RealityKings - Carolina Guerrero - Sneaky Roommate Steals Boyfriend (08.05.2026) rq.mp4",
    "FamilyTherapyXXX - Ashley Alexander - Natural (29.04.2026) rq.mp4",
    "Tushy - Melanie Marie - Pull Chapter 3 Pinned (17.05.2026) rq.mp4",
    "MySistersHotFriend - Ivy Mayhem (07.05.2026) rq.mp4",
    "Brazzers - Anissa Kate, Siri Dahl - The Rizz Chronicles My Stepmom's BFF (09.05.2026) rq.mp4",
    "FillUpMyMom - Cherry Kiss - Is a Cool Stepmom (25.04.2026) rq.mp4",
    "Cum4K - Autumn Falls - Creeping Stepdaughter Creamed rq.mp4",
    "MomWantsToBreed - Bunny Madison - Stepmom And I Celebrate Manuary (16.05.2026) rq.mp4",
    "NFBusty - Amalia Davis - He Knows How To Take Care Of Me (15.05.2026) rq.mp4",
    "Brazzers - Angela White - Two For Her Pleasure (29.04.2026) rq.mp4",
    "Fansly - Jessie Rogers - Fucking My Cheating Exs Bestfriend rq.mp4",
    "Horny Amateur Girlfriend With Beautiful Natural Tits Cheats On Boyfriend [MP4-1080p]",
    "JulesJordan - Valentina Nappi - Soaking Wet rq.mp4",
    "TushyRaw - Dolly Orchid - Pretty lil Snack Has Her Tiny Ass Stretched Out (26.04.2026) rq.mp4",
    "Tushy - Ariana Van X - Natural Beautys Tight Ass Gets Filled In Tushy Debut (03.05.2026) rq.mp4",
    "WowGirls 22 01 31 Eva Elfie And Kate Rich Double Flame XXX 480p MP4-XXX [XC]",
    "BlackedRaw - Agatha Vega, Ella Hughes - Knockout Babes Fuck Two Cops On Duty (16.05.2026) rq.mp4",
    "SisSwap - Lulu Chu, Penelope Woods (17.05.2026) rq.mp4",
    "Blacked - Cecelia Taylor - Married Blonde Ditches Hubby For BBC (08.05.2026) rq.mp4",
    "Vixen - Kate Dalia - Hot Divorcee Gets A Fuck Shes Been Missing (17.05.2026) rq.mp4",
    "NewSensations 26 05 09 Koda Monroe XXX 1080p MP4-WRB [XC]",
    "NuruMassage - Ellie Nova - Hubby's Risky Rendezvous (23.02.2026) rq.mp4",
    "Julia Fit - Anal Chronicles: Her Ass Sucked a Dick [MP4-1080p]",
    "SingleMoms - Bunny Madison (05.05.2026) rq.mp4",
    "Blacked - Lucy Mochi - Petite Cheater Gets Stretched To Her Limit (18.05.2026) rq.mp4",
    "CzechBoobs 26 05 11 Amber Bloom XXX 1080p MP4-WRB [XC]",
    "FamilySwap - Amirah Adara, Isabella Jules - My Swap Family Is Closer Than Ever (21.05.2026) rq.mp4",
    "Freeze - Veronica Leal - Magic Dart (15.05.2026) rq.mp4",
    # ── Real Library Failing Torrents added to test suite ─────────────────────
    "BellesaBlindDate.26.05.08.E187.Andi.Avalon.And.Musa.XXX.1080p.MP4-P2P[XC]",
    "FuckPassVR.22.10.24.Ariana.Joy.Family.Business.in.Lviv.XXX.VR180.4096p.MP4-Narcos[XC]",
    "HotMilfsFuck - Selene Love - Use And Abuse Me (09.11.2026) rq.mp4",
    "PrivateSociety.26.05.19.Stephanie.XXX.1080p.MP4-WRB[XC]",
]

# ── Stats tracking ────────────────────────────────────────────────────────────
import concurrent.futures

# Suppress debug log spam during concurrent execution to prevent terminal interleaving
main.log = lambda msg: None

def test_single_title(title: str):
    parsed = main.parse_adult_filename(title)
    res = main.adult_enrichment_lookup(title)
    return {
        "title": title,
        "parsed": parsed,
        "result": res
    }

passed = []
failed = []

print(f"\n{'='*80}")
print(f"  STRESS TEST: {len(test_titles)} torrent names (PARALLEL MODE)")
print(f"{'='*80}\n")

# Run up to 100 parallel lookups
with concurrent.futures.ThreadPoolExecutor(max_workers=100) as executor:
    results = list(executor.map(test_single_title, test_titles))

for i, r in enumerate(results, 1):
    title = r["title"]
    parsed = r["parsed"]
    res = r["result"]

    print(f"\n[{i:02d}/{len(test_titles)}] {title[:80]}{'...' if len(title)>80 else ''}")
    print(f"  {'─'*60}")
    print(f"  Parsed → site={parsed.get('site')!r}  date={parsed.get('date')!r}  name={repr(parsed.get('name')):.40s}")

    if res:
        matched_title = res.get('Title', 'N/A')
        year = res.get('Year', 'N/A')
        rid = res.get('imdbID', 'N/A')
        print(f"  ✅ MATCH: {matched_title} ({year}) → {rid}")
        passed.append((title, matched_title))
    else:
        print(f"  ❌ NO MATCH")
        failed.append(title)

# ── Summary ───────────────────────────────────────────────────────────────────
print(f"\n{'='*80}")
print(f"  RESULTS: {len(passed)}/{len(test_titles)} matched  ({100*len(passed)//len(test_titles)}%)")
print(f"{'='*80}")
if failed:
    print(f"\n  ── FAILED ({len(failed)}) ──────────────────────────────────────")
    for t in failed:
        print(f"  ✗ {t[:100]}")
if passed:
    print(f"\n  ── PASSED ({len(passed)}) ──────────────────────────────────────")
    for orig, matched in passed:
        print(f"  ✓ {orig[:55]:<55} → {matched}")

import os
os._exit(0)


