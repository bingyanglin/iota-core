import matplotlib.pyplot as plt
from datetime import datetime, timedelta, timezone
import pandas as pd
import seaborn as sns
import numpy as np
import re
import os
import json

RESULT = []

OUTPUT_COLUMNS = [
    'LivenessThresholdLowerBoundInSeconds', 
    'LivenessThresholdUpperBoundInSeconds',
    'MinCommittableAge', 
    'MaxCommittableAge', 
    'EpochNearingThreshold',
    'blockPreAccepted mean', 
    'blockPreAccepted var', 
    'blockPreAccepted median', 
    'blockPreAccepted count',
    'blockAccepted mean', 
    'blockAccepted var', 
    'blockAccepted median', 
    'blockAccepted count',
    'blockPreConfirmed mean', 
    'blockPreConfirmed var', 
    'blockPreConfirmed median', 
    'blockPreConfirmed count',
    'blockConfirmed mean', 
    'blockConfirmed var', 
    'blockConfirmed median', 
    'blockConfirmed count',
    'blockScheduled mean', 
    'blockScheduled var', 
    'blockScheduled median', 
    'blockScheduled count',
    'unConfirmedYet mean',
    'unConfirmedYet var',
    'unConfirmedYet median',
    'unConfirmedYet count',
    'unConfirmedYet max',
    'unConfirmedYet 1min',
    'unConfirmedYet 1min Over Total Scheduled',
    'totalConfirmed Over Total Scheduled',
    'totalScheduled',
    'totalConfirmed'
]

class Block():
    def __init__(self,
                 log_timestamp,
                 issued_timestamp, 
                 scheduled_timestamp,
                 scheduling_time,
                 pre_acceptance_time,
                 acceptance_time,
                 pre_confirmed_time,
                 confirmed_time):
        self.log_timestamp = log_timestamp
        self.issued_timestamp = issued_timestamp
        self.scheduled_timestamp = scheduled_timestamp
        self.scheduling_time = scheduling_time
        self.pre_acceptance_time = pre_acceptance_time
        self.acceptance_time = acceptance_time
        self.pre_confirmed_time = pre_confirmed_time
        self.confirmed_time = confirmed_time
        self.unconfirmed_time_yet_since_issuance_time = None

    def set_unconfirmed_time_yet_since_issuance_time(self, unconfirmed_time_yet_since_issuance_time):
        self.unconfirmed_time_yet_since_issuance_time = unconfirmed_time_yet_since_issuance_time

def convert_to_datetime(timestamp_str):
    # Splitting the timestamp into the main part and nanoseconds
    main_part, nanoseconds = timestamp_str[:-1].split('.')
    
    # Parsing the main part of the timestamp
    dt = datetime.strptime(main_part, '%Y-%m-%dT%H:%M:%S')

    # Adding nanoseconds (as microseconds) to the datetime object
    nanoseconds = int(nanoseconds) // 1000  # Convert nanoseconds to microseconds
    dt = dt.replace(microsecond=nanoseconds, tzinfo=timezone.utc)

    return dt

def calculate_difference_in_ns(datetime1, datetime2):
    difference = datetime2 - datetime1
    nanoseconds = ((difference.days * 24 * 60 * 60 * 1e9) + 
                   (difference.seconds * 1e9) + 
                   (difference.microseconds * 1e3))
    return abs(nanoseconds)


def parse_timestamp(ts):
    try:
        # Try parsing the standard format
        return datetime.fromisoformat(ts.replace('Z', '+00:00'))
    except ValueError:
        # Handle the case where the fractional seconds are too long
        parts = ts.split('.')
        datetime_part, milliseconds = parts[0], parts[1]
        datetime_object = datetime.strptime(datetime_part, '%Y-%m-%dT%H:%M:%S')
        fractional_seconds = float('0.' + milliseconds.rstrip('Z'))  # Convert fractional part to seconds
        return datetime_object + timedelta(seconds=fractional_seconds)


def plot_data(blocks, path):
    types_titles = ['Scheduled', 'Pre Accepted', 'Accepted', 'PreConfirmed', 'Confirmed', 'Unconfirmed Yet']
    types = []
    values = []
    for _, info in blocks.items():
        if info.scheduling_time is not None:
            values.append(info.scheduling_time)
            types.append(types_titles[0])
        if info.pre_acceptance_time is not None:
            values.append(info.pre_acceptance_time)
            types.append(types_titles[1])
        if info.acceptance_time is not None:
            values.append(info.acceptance_time)
            types.append(types_titles[2])
        if info.pre_confirmed_time is not None:
            values.append(info.pre_confirmed_time)
            types.append(types_titles[3])
        if info.confirmed_time is not None:
            values.append(info.confirmed_time)
            types.append(types_titles[4])
        if info.unconfirmed_time_yet_since_issuance_time is not None:
            values.append(info.unconfirmed_time_yet_since_issuance_time)
            types.append(types_titles[5])
    
    # Create a DataFrame for Seaborn
    df = pd.DataFrame({'Type': types, 'Values': values})

    # Plotting the violin plot
    plt.figure(figsize=(10, 5))
    sns.violinplot(x='Type', y='Values', data=df, hue="Type", legend=False, linewidth=2, width=0.8, cut=0, order=types_titles)
    plt.xlabel('Type', fontname='Times New Roman', fontsize=18, fontweight='bold')
    plt.ylabel('Time (s)', fontname='Times New Roman', fontsize=18, fontweight='bold')
    plt.xticks(fontname='Times New Roman', fontsize=16)
    plt.yticks(fontname='Times New Roman', fontsize=16)
    plt.tight_layout()
    plt.savefig(f'{path}_new.png')


def parse_config(config_path):
    # Parameters to extract
    params = ["LivenessThresholdLowerBoundInSeconds", 
            "LivenessThresholdUpperBoundInSeconds", 
            "MinCommittableAge", 
            "MaxCommittableAge", 
            "EpochNearingThreshold"]

    # Function to extract and format the data into tables
    with open(config_path, 'r') as file:
        data_lines = [line.strip() for line in file if line.strip()]
        tables = []
        for line in data_lines:
            match = re.search(r"WithLivenessOptions\((.*?)\)", line)
            if match:
                values = match.group(1).split(", ")
                table = {param: int(value) for param, value in zip(params, values)}
                tables.append(table)
        return tables

def parse_log_file(file_path):
    print(f'Processing {file_path}...')
    with open(file_path, 'r') as file:
        lines = file.readlines()

        # Parse the logs and put them into the block list
        end_timestamp = datetime(1900,1,1,0,0,0,0,timezone.utc)
        min_timestamp = datetime(2100,1,1,0,0,0,0,timezone.utc)
        blocks = {}
        for line in lines:
            line = line.replace('@', '')
            log_entry = json.loads(line)
            log_timestamp = convert_to_datetime(log_entry.get("timestamp", ""))
            log_entry = log_entry.get("log", {})
            issued_timestamp = log_entry.get("issuedTimestamp", None)
            if issued_timestamp is None:
                continue
            issued_timestamp = convert_to_datetime(issued_timestamp)

            if log_timestamp > end_timestamp:
                end_timestamp = log_timestamp
            if log_timestamp < min_timestamp:
                min_timestamp = log_timestamp

            block_id = log_entry.get("blockID", "")
            block_type = log_entry.get("type", "")
            if (block_type == "blockScheduled" or 
                block_type == "blockPreAccepted" or 
                block_type == "blockAccepted" or 
                block_type == "blockPreConfirmed" or 
                block_type == "blockConfirmed"):
                
                abnormal_checking = log_entry.get("scheduledTimestamp", "")
                if abnormal_checking == '0001-01-01T00:00:00Z' or abnormal_checking == '':
                    continue
                scheduled_timestamp = convert_to_datetime(log_entry.get("scheduledTimestamp", ""))
                scheduling_time = log_entry.get("schedulingTime", "")
                pre_acceptance_time = log_entry.get("preAcceptanceTime", "")
                acceptance_time = log_entry.get("acceptanceTime", "")
                pre_confirmed_time = log_entry.get("preConfirmedTime", "")
                confirmed_time = log_entry.get("confirmedTime", "")

                if block_type == "blockScheduled":
                    scheduling_time /= 1e9
                    pre_acceptance_time = None
                    acceptance_time = None
                    pre_confirmed_time = None
                    confirmed_time = None
                elif block_type == "blockPreAccepted":
                    scheduling_time /= 1e9
                    pre_acceptance_time /= 1e9
                    acceptance_time = None
                    pre_confirmed_time = None
                    confirmed_time = None
                elif block_type == "blockAccepted":
                    scheduling_time /= 1e9
                    acceptance_time /= 1e9
                    pre_acceptance_time = None
                    pre_confirmed_time = None
                    confirmed_time = None
                elif block_type == "blockPreConfirmed":
                    scheduling_time /= 1e9
                    pre_confirmed_time /= 1e9
                    acceptance_time = None
                    pre_acceptance_time = None
                    confirmed_time = None
                elif block_type == "blockConfirmed":
                    scheduling_time /= 1e9
                    confirmed_time /= 1e9
                    acceptance_time = None
                    pre_acceptance_time = None
                    pre_confirmed_time = None

                # Create a new block
                if block_id not in blocks:
                    blocks[block_id] = Block(log_timestamp,
                                            issued_timestamp, 
                                            scheduled_timestamp, 
                                            scheduling_time, 
                                            pre_acceptance_time, 
                                            acceptance_time, 
                                            pre_confirmed_time, 
                                            confirmed_time)
                # Update the existing block
                else:
                    blocks[block_id].issued_timestamp = issued_timestamp if issued_timestamp is not None else None
                    blocks[block_id].scheduled_timestamp = scheduled_timestamp if scheduled_timestamp is not None else None
                    blocks[block_id].scheduling_time = scheduling_time if scheduling_time is not None else None
                    blocks[block_id].pre_acceptance_time = pre_acceptance_time if pre_acceptance_time is not None else None
                    blocks[block_id].acceptance_time = acceptance_time if acceptance_time is not None else None
                    blocks[block_id].pre_confirmed_time = pre_confirmed_time if pre_confirmed_time is not None else None
                    blocks[block_id].confirmed_time = confirmed_time if confirmed_time is not None else None
            else:
                continue


        # Drop the blocks whose log timestamp is less than 60s after the min timestamp
        blocks_to_drop = []
        for block_id, block in blocks.items():
            if calculate_difference_in_ns(min_timestamp, block.log_timestamp) < 61 * 1e9:
                blocks_to_drop.append(block_id)
        for block_id in blocks_to_drop:
            blocks.pop(block_id, None)

        # Calculate the unconfirmed time yet since issuance time
        for block_id, block in blocks.items():
            if block.confirmed_time is None:
                unconfirmed_time_yet_since_issuance_time = calculate_difference_in_ns(
                    block.issued_timestamp, end_timestamp)
                block.set_unconfirmed_time_yet_since_issuance_time(unconfirmed_time_yet_since_issuance_time/1e9)

    return blocks

def get_single_result(table, log_path, plot=False):
    temp_data = dict(table)

    # If log file exists, process it
    if os.path.exists(log_path): 
        blocks = parse_log_file(log_path)
        statistics = {"blockScheduled": [], 
                        "blockPreAccepted" : [], 
                        "blockAccepted": [], 
                        "blockPreConfirmed": [],  
                        "blockConfirmed": [], 
                        "unConfirmedYet": []}
        
        if plot:
            fig_path = log_path.split('.log')[0]
            plot_data(blocks, fig_path)
        
        for _, block in blocks.items():
            if block.scheduling_time is not None:
                statistics["blockScheduled"].append(block.scheduling_time)
            if block.pre_acceptance_time is not None:
                statistics["blockPreAccepted"].append(block.pre_acceptance_time)
            if block.acceptance_time is not None:
                statistics["blockAccepted"].append(block.acceptance_time)
            if block.pre_confirmed_time is not None:
                statistics["blockPreConfirmed"].append(block.pre_confirmed_time)
            if block.confirmed_time is not None:
                statistics["blockConfirmed"].append(block.confirmed_time)
            if block.unconfirmed_time_yet_since_issuance_time is not None:
                statistics["unConfirmedYet"].append(block.unconfirmed_time_yet_since_issuance_time)

        for key, val in statistics.items():
            temp_data[f'{key} mean'] = np.mean(val) if val else None
            temp_data[f'{key} var'] = np.var(val) if val else None
            temp_data[f'{key} median'] = np.median(val) if val else None
            temp_data[f'{key} count'] = len(val) if val else None
        temp_data['unConfirmedYet max'] = max(statistics["unConfirmedYet"]) if statistics["unConfirmedYet"] else None
        temp_data['totalScheduled'] = len(statistics["blockScheduled"])
        temp_data['totalConfirmed'] = len(statistics["blockConfirmed"])

        # Get the number of blocks whose unconfirmed time yet since issuance time is > 1 minute
        temp_data['unConfirmedYet 1min'] = len([val for val in statistics["unConfirmedYet"] if val > 60])
        temp_data['unConfirmedYet 1min Over Total Scheduled'] = float(temp_data['unConfirmedYet 1min']) / float(temp_data['totalScheduled'])
        temp_data['totalConfirmed Over Total Scheduled'] = float(temp_data['totalConfirmed']) / float(temp_data['totalScheduled'])
    
    return temp_data

if __name__ == "__main__":
    PLOT = True
    log_folders = ['2023_11_22_12_03', 
                   '2023_11_23_14_58',
                   '2023_11_25_16_35',
                   '2023_11_26_12_43',
                   '2023_11_27_16_19']
    for log_folder in log_folders:
        folder_path = f'../../../profiling_results/{log_folder}'
        print(f'Parsing {folder_path}...')

        # Get the table from the input file
        tables = parse_config(f'{folder_path}/input.txt')

        for i, table in enumerate(tables, 0):
            log_path = f'{folder_path}/{i}.log'
            temp_data = get_single_result(table, log_path, PLOT)
            RESULT.append(temp_data)

    # Create DataFrame from the list of dictionaries
    df = pd.DataFrame(RESULT, columns=OUTPUT_COLUMNS)
    df.to_csv('result_new.csv', index=False)