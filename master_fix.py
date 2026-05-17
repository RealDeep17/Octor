import os
import re

root_dir = os.path.dirname(os.path.abspath(__file__))

def safe_replace(path, pattern, replacement):
    if not os.path.exists(path):
        return
    with open(path, "r") as f:
        content = f.read()
    new_content = re.sub(pattern, replacement, content)
    if content != new_content:
        print(f"Updating {path}")
        with open(path, "w") as f:
            f.write(new_content)

# 1. Standardize proto packages
for root, dirs, files in os.walk(root_dir):
    for f in files:
        if f.endswith(".pb.go"):
            safe_replace(os.path.join(root, f), r"package __", "package proto")

# 2. Fix magnet2torrent imports in rest-api
rest_api_dir = os.path.join(root_dir, "rest-api")
for root, dirs, files in os.walk(rest_api_dir):
    for f in files:
        if f.endswith(".go"):
            safe_replace(os.path.join(root, f), r"github\.com/webtor-io/magnet2torrent/magnet2torrent", "github.com/webtor-io/magnet2torrent/proto")

# 3. Apply MKV trick to web-ui/jobs/scripts/action.go
# (Note: I'll just re-run the previous replacement logic but safely)
action_go_path = os.path.join(root_dir, "web-ui", "jobs", "scripts", "action.go")

# Adding directPlayURL
safe_replace(action_go_path, 
             r"func contentProbeURL\(downloadURL string\) string \{", 
             "func contentProbeURL(downloadURL string) string {\n}\n\nfunc directPlayURL(downloadURL string) string {\n\tu, err := url.Parse(downloadURL)\n\tif err != nil {\n\t\treturn downloadURL\n\t}\n\tq := u.Query()\n\tq.Del(\"download\")\n\tu.RawQuery = q.Encode()\n\treturn u.String()\n}\n\nfunc _contentProbeURL_stub(downloadURL string) string {")
# Note: the above is a bit messy but I'll use a more precise replacement if I can.

# Actually, I'll just use the exact content from previous turns for safety.
