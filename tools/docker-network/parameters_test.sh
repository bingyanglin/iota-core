#!/bin/bash

set -x

# Path to the input file and the presets.go file
input_file="input.txt"
presets_file="../genesis-snapshot/presets/presets.go"
original_presets_file="../genesis-snapshot/presets/presets.go.bak"

# Backup the original presets file
cp "$presets_file" "$original_presets_file"

# Temporary file to hold the current block
temp_file=$(mktemp)

# Function to check HTTP status code
check_status() {
    local url=$1
    local status_code=$(curl -o /dev/null -s -w "%{http_code}\n" "$url")
    if [ "$status_code" -eq 200 ]; then
        return 0 # success
    else
        return 1 # failure
    fi
}

# Function to check all ports
check_all_ports() {
    # List of ports to check
    ports=(8050 8060 8070 8080 8090)
    for port in "${ports[@]}"; do
        if ! check_status "http://localhost:$port/health"; then
            return 1 # if any check fails, return failure
        fi
    done
    return 0 # if all checks pass, return success
}

run_network() {
    echo "Running network..."
    # ./run.sh >/dev/null 2>&1 &
    ./run.sh >/dev/null &
    RUN_PID=$!
}

check_network() {
    # Maximum wait time in seconds (5 minutes)
    max_wait=300
    start_time=$(date +%s)

    # Retry loop with timeout
    while true; do
        if check_all_ports; then
            echo "All checks passed. Continuing with the script..."
            # Your additional script logic goes here
            break
        else
            current_time=$(date +%s)
            elapsed=$((current_time - start_time))
            if [ "$elapsed" -ge "$max_wait" ]; then
                echo "Timeout reached. One or more checks failed. Aborting the script."
                return 1
            fi
            echo "One or more checks failed. Retrying..."
            sleep 5 # Wait for 5 seconds before retrying
        fi
    done
}

run_tests() {
    echo "=> Running tests..."
    # Your test logic goes here
    cd tool
    timeout 1m ./evil-tools spammer -urls "http://localhost:8050" -spammer blk -rate 1000 -duration 10m
    cd -

    echo "Waiting a little..."
    sleep 10
}

stop_network() {
    echo "Force-stop the network..."
    docker compose kill
}

# Function to process and replace a block
process_block() {
    # Replace PARAMETERS_GOES_HERE with the current block
    #sed "/PARAMETERS_GOES_HERE/r $temp_file" -e "/PARAMETERS_GOES_HERE/d" "$original_presets_file" > "$presets_file"
    awk -v block="$(<"$temp_file")" '/PARAMETERS_GOES_HERE/ {print block; next} {print}' "$original_presets_file" > "$presets_file"

    run_network

    if ! check_network; then
        echo "Network failed to sync."
        exit 1
    fi

    run_tests

    stop_network
    
    # Wait for the network to stop
    wait $RUN_PID

    # Restore the original presets file for the next iteration
    cp "$original_presets_file" "$presets_file"
}

# Read the input file and process it block by block
while IFS= read -r line || [[ -n "$line" ]]; do
    # Check for empty line indicating end of a block
    if [[ -z "$line" ]]; then
        echo "Processing block..."
        process_block
        # Clear the temp file for the next block
        > $temp_file
    else
        # Append line to temp file
        echo "$line" >> $temp_file
    fi
done < "$input_file"

# Process the last block if the file does not end with a newline
if [[ -s "$temp_file" ]]; then
    echo "Processing final block..."
    process_block
fi

# Clean up
rm $temp_file
rm "$original_presets_file"

echo "All blocks processed and substituted in $presets_file."
