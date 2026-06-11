import os

# Keep imports deterministic and avoid live API calls during pytest collection.
os.environ.setdefault("SIDECAR_ENRICHMENT_ENABLED", "false")

import main


def test_parse_dash_separated_site_performer_title_date():
    parsed = main.parse_adult_filename(
        "Brazzers - Angela White - Two For Her Pleasure (29.04.2026) rq.mp4"
    )

    assert parsed["site"] == "Brazzers"
    assert parsed["performer"] == "Angela White"
    assert parsed["name"] == "Two For Her Pleasure"
    assert parsed["date"] == "2026-04-29"


def test_parse_dot_separated_release_extracts_known_studio():
    parsed = main.parse_adult_filename(
        "Blacked.24.08.05.Kendra.Sunderland.Here.To.Stay.XXX.1080p.HEVC.x265.PRT[XvX]"
    )

    assert parsed["site"] == "Blacked"
    assert "Kendra Sunderland" in parsed["name"]
    assert parsed["date"] == "2024-08-05"


def test_studio_detection_uses_known_adult_studios():
    assert main.studio_in_title("TushyRaw - Dolly Orchid - Pretty lil Snack")
    assert not main.studio_in_title("Ubuntu 24.04 server install guide")
