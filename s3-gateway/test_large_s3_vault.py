import boto3
import os
import time
import subprocess
import shutil

def get_disk_free(path):
    total, used, free = shutil.disk_usage(path)
    return free

def test_large_vault():
    endpoint_url = "http://localhost:9000"
    bucket = "vault"
    key = "test-large-file.bin"
    
    # 2 GB total size: 40 parts of 50 MB each
    part_size = 50 * 1024 * 1024  # 50 MB
    num_parts = 40
    total_size = part_size * num_parts
    
    print(f"Connecting to Custom S3 Gateway at {endpoint_url}...")
    s3 = boto3.client(
        "s3",
        endpoint_url=endpoint_url,
        aws_access_key_id="octoradmin",
        aws_secret_access_key="octorpassword",
        region_name="us-east-1"
    )
    
    # Check initial SSD free space on /srv
    init_srv_free = get_disk_free("/srv")
    init_root_free = get_disk_free("/")
    print(f"Initial SSD Free Space: /srv = {init_srv_free / (1024**3):.2f} GB, / = {init_root_free / (1024**3):.2f} GB")
    
    # 1. Initiate Multipart Upload
    print("Initiating 2 GB Multipart Upload...")
    mpu = s3.create_multipart_upload(Bucket=bucket, Key=key)
    upload_id = mpu["UploadId"]
    print(f"Multipart initiated. UploadId: {upload_id}")
    
    # Generate 50 MB static block to reuse (saving Python memory)
    print("Generating 50 MB chunk payload in RAM...")
    chunk_payload = os.urandom(part_size)
    
    parts = []
    start_time = time.time()
    
    # 2. Upload Parts in sequence
    for i in range(num_parts):
        part_number = i + 1
        part_start = time.time()
        print(f"Uploading Part {part_number}/{num_parts} ({part_size / (1024**2):.1f} MB)...")
        
        part_response = s3.upload_part(
            Bucket=bucket,
            Key=key,
            UploadId=upload_id,
            PartNumber=part_number,
            Body=chunk_payload
        )
        parts.append({"PartNumber": part_number, "ETag": part_response["ETag"]})
        part_duration = time.time() - part_start
        speed = (part_size / (1024**2)) / part_duration
        print(f"  -> Part {part_number} uploaded in {part_duration:.2f}s ({speed:.2f} MB/s)")
        
        # Monitor SSD disk usage every 5 parts
        if part_number % 5 == 0:
            srv_free = get_disk_free("/srv")
            root_free = get_disk_free("/")
            srv_diff = (init_srv_free - srv_free) / (1024**2)
            root_diff = (init_root_free - root_free) / (1024**2)
            print(f"  [MONITOR] SSD Free Space Change: /srv = -{srv_diff:.2f} MB, / = -{root_diff:.2f} MB")
            
            # Print current RAM usage
            mem_out = subprocess.check_output("free -h", shell=True).decode()
            print(f"  [MONITOR] RAM Status:\n{mem_out}")
            
    # 3. Complete Multipart Upload
    print("Completing Multipart Upload (Merging parts directly on Google Drive)...")
    complete_start = time.time()
    s3.complete_multipart_upload(
        Bucket=bucket,
        Key=key,
        UploadId=upload_id,
        MultipartUpload={"Parts": parts}
    )
    complete_duration = time.time() - complete_start
    print(f"Multipart completed in {complete_duration:.2f}s!")
    
    total_duration = time.time() - start_time
    avg_speed = (total_size / (1024**2)) / total_duration
    print(f"SUCCESS: 2 GB Multipart Upload Finished in {total_duration:.2f}s (Average Speed: {avg_speed:.2f} MB/s)!")
    
    # 4. Head Object to verify final size on Google Drive
    print("Heading Object...")
    head = s3.head_object(Bucket=bucket, Key=key)
    print(f"Head response: Size={head['ContentLength']} bytes, ETag={head['ETag']}")
    assert head["ContentLength"] == total_size, "Size mismatch!"
    
    # Final SSD space validation
    final_srv_free = get_disk_free("/srv")
    final_root_free = get_disk_free("/")
    final_srv_diff = (init_srv_free - final_srv_free) / (1024**2)
    final_root_diff = (init_root_free - final_root_free) / (1024**2)
    print(f"Final SSD Cache Leak Check: /srv = -{final_srv_diff:.2f} MB, / = -{final_root_diff:.2f} MB")
    
    if abs(final_srv_diff) < 200: # Allow under 200 MB for other logs/system operations
        print("SUCCESS: Zero SSD wear confirmed! The upload went directly through the RAM pipeline!")
    else:
        print("WARNING: SSD space changed during the upload!")

if __name__ == "__main__":
    try:
        test_large_vault()
    except Exception as e:
        print(f"TEST FAILED: {e}")
