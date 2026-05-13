import os
import re

root_dir = r"e:\project\octor\rest-api"
for root, dirs, files in os.walk(root_dir):
    for f in files:
        if f.endswith(".go"):
            path = os.path.join(root, f)
            with open(path, "r") as file:
                content = file.read()
            new_content = re.sub(r"github\.com/webtor-io/magnet2torrent/magnet2torrent", "github.com/webtor-io/magnet2torrent/proto", content)
            if content != new_content:
                print(f"Updating {path}")
                with open(path, "w") as file:
                    file.write(new_content)
