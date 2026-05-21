#!/bin/bash
res="d7fc6daa869f8adf30a7b79b723042fb28ddcf18"
s1=$(curl -s http://localhost:8086/resource/$res | jq -r .stored_size)
sleep 10
s2=$(curl -s http://localhost:8086/resource/$res | jq -r .stored_size)
diff=$((s2 - s1))
mb=$(echo "scale=2; $diff / 1048576 / 10" | bc)
echo "Current Speed: $mb MB/s"
