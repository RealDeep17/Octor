import requests
import time
import statistics

API_KEY = "ef2a909b564245b8a2849325e29146d3"
BASE_URL = "http://localhost:9696/prowlarr/api/v1"
HEADERS = {"X-Api-Key": API_KEY}

# List of indexer IDs and names to test
INDEXERS = [
    (1, "The Pirate Bay"),
    (2, "Nyaa.si"),
    (3, "1337x"),
    (10, "sukebei.nyaa.si"),
    (12, "EXT Torrents"),
    (13, "Tokyo Toshokan"),
    (17, "BitSearch"),
    (16, "RuTracker (Cookies)"),
    (8, "XXXClub"),
    (5, "TorrentGalaxyClone")
]

QUERIES = ["star wars", "slime", "avatar", "mida", "boys", "marvel", "demon", "batman", "one piece", "shogun"]

def run_test():
    stats = {}
    for idx_id, name in INDEXERS:
        print(f"Testing {name}...")
        stat = {"times": [], "results": 0, "failures": 0}
        
        for query in QUERIES:
            start_time = time.time()
            try:
                # Search using the Prowlarr API
                r = requests.get(f"{BASE_URL}/search", headers=HEADERS, params={
                    "query": query,
                    "indexerIds": [idx_id],
                    "limit": 20
                }, timeout=60)
                
                elapsed = time.time() - start_time
                if r.status_code == 200:
                    data = r.json()
                    stat["times"].append(elapsed)
                    stat["results"] += len(data)
                else:
                    stat["failures"] += 1
            except Exception as e:
                stat["failures"] += 1
            
            time.sleep(1) # Gap between queries for stability
        
        stats[name] = stat

    print("\n--- PERFORMANCE TEST RESULTS (10 QUERY TEST) ---")
    print(f"{'Indexer':<25} | {'Avg Speed':<10} | {'Total Results':<12} | {'Reliability':<10} | {'Score'}")
    print("-" * 75)
    
    for name, data in stats.items():
        avg_speed = statistics.mean(data["times"]) if data["times"] else 0
        total_results = data["results"]
        reliability = (len(data["times"]) / len(QUERIES)) * 100
        
        # Scoring logic: 
        # Results (max 40) + Speed (max 40, <2s is best) + Reliability (max 20)
        speed_score = max(0, 40 - (avg_speed * 5)) # 0s=40pts, 8s=0pts
        result_score = min(40, (total_results / 100) * 40) # 100+ total results = 40pts
        rel_score = (reliability / 100) * 20
        
        total_score = speed_score + result_score + rel_score
        
        print(f"{name:<25} | {avg_speed:>8.2f}s | {total_results:>12} | {reliability:>10.0f}% | {total_score:>5.1f}/100")

if __name__ == "__main__":
    run_test()
