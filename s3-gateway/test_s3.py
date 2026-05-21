import boto3
import os
import hashlib

def calculate_md5(data):
    return hashlib.md5(data).hexdigest()

def test_s3_gateway():
    endpoint_url = "http://localhost:9000"
    bucket = "vault"
    key = "test-multipart-file.bin"
    
    print(f"Connecting to Custom S3 Gateway at {endpoint_url}...")
    s3 = boto3.client(
        "s3",
        endpoint_url=endpoint_url,
        aws_access_key_id="octoradmin",
        aws_secret_access_key="octorpassword",
        region_name="us-east-1"
    )
    
    # 1. Generate test data (15 MB)
    print("Generating 15 MB of random test data...")
    part_size = 5 * 1024 * 1024  # 5 MB
    total_size = part_size * 3
    data = os.urandom(total_size)
    original_md5 = calculate_md5(data)
    
    # 2. Initiate Multipart Upload
    print("Initiating Multipart Upload...")
    mpu = s3.create_multipart_upload(Bucket=bucket, Key=key)
    upload_id = mpu["UploadId"]
    print(f"Multipart upload initiated. UploadId: {upload_id}")
    
    # 3. Upload Parts
    parts = []
    for i in range(3):
        part_number = i + 1
        part_data = data[i * part_size : (i + 1) * part_size]
        print(f"Uploading Part {part_number} (Size: {len(part_data)} bytes)...")
        part_response = s3.upload_part(
            Bucket=bucket,
            Key=key,
            UploadId=upload_id,
            PartNumber=part_number,
            Body=part_data
        )
        parts.append({"PartNumber": part_number, "ETag": part_response["ETag"]})
        print(f"Part {part_number} uploaded. ETag: {part_response['ETag']}")
        
    # 4. Complete Multipart Upload
    print("Completing Multipart Upload...")
    s3.complete_multipart_upload(
        Bucket=bucket,
        Key=key,
        UploadId=upload_id,
        MultipartUpload={"Parts": parts}
    )
    print("Multipart upload completed successfully!")
    
    # 5. Head Object to verify metadata
    print("Heading Object...")
    head = s3.head_object(Bucket=bucket, Key=key)
    print(f"Head response: Size={head['ContentLength']} bytes, ETag={head['ETag']}")
    assert head["ContentLength"] == total_size, "Size mismatch!"
    
    # 6. Download Object to verify integrity
    print("Downloading Object back for integrity check...")
    downloaded = s3.get_object(Bucket=bucket, Key=key)
    downloaded_data = downloaded["Body"].read()
    downloaded_md5 = calculate_md5(downloaded_data)
    
    print(f"Original MD5:   {original_md5}")
    print(f"Downloaded MD5: {downloaded_md5}")
    
    assert original_md5 == downloaded_md5, "MD5 hash mismatch! Data corruption detected!"
    print("SUCCESS: Data integrity verified perfectly! MD5 hashes match 100%!")

if __name__ == "__main__":
    try:
        test_s3_gateway()
    except Exception as e:
        print(f"TEST FAILED: {e}")
