import os
import re

root_dir = os.path.dirname(os.path.abspath(__file__))
repos = [d for d in os.listdir(root_dir) if os.path.isdir(os.path.join(root_dir, d))]

def localize_go_mod(repo_path):
    go_mod_path = os.path.join(repo_path, "go.mod")
    if not os.path.exists(go_mod_path):
        return

    with open(go_mod_path, "r") as f:
        content = f.read()

    # Find all webtor-io dependencies
    deps = re.findall(r"github\.com/webtor-io/([\w-]+)", content)
    
    replaces = []
    for dep in set(deps):
        if dep in repos:
            # Skip replacing itself
            if os.path.basename(repo_path) == dep:
                continue
            
            # Calculate relative path
            # Since all repos are in the same root_dir, it's just ../{dep}
            rel_path = f"../{dep}"
            replace_line = f"replace github.com/webtor-io/{dep} => {rel_path}"
            
            if replace_line not in content:
                replaces.append(replace_line)

    if replaces:
        print(f"Localizing {go_mod_path}...")
        with open(go_mod_path, "a") as f:
            f.write("\n" + "\n".join(replaces) + "\n")

for repo in repos:
    localize_go_mod(os.path.join(root_dir, repo))
