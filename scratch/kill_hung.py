import os, subprocess

out = subprocess.check_output("ps aux", shell=True).decode()
for line in out.splitlines():
    if "curl" in line or "journalctl" in line or "df -h" in line:
        parts = line.split()
        pid = parts[1]
        print(f"Killing {pid}: {line}")
        os.system(f"kill -9 {pid}")
