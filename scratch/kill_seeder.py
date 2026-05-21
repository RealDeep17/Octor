import os, subprocess

out = subprocess.check_output("ps aux", shell=True).decode()
for line in out.splitlines():
    if "torrent-web-see" in line or "systemctl restart" in line:
        parts = line.split()
        pid = parts[1]
        print(f"Killing {pid}: {line}")
        os.system(f"kill -9 {pid}")
