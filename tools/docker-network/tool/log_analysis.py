import matplotlib.pyplot as plt
from datetime import datetime, timedelta
import numpy as np
import pandas as pd
import seaborn as sns
import json

def process_log_file(file_path):
    # Initialize a dictionary to hold the values
    data = {
        "blockPreAccepted": [],
        "blockAccepted": [],
        "blockPreConfirmed": [],
        "blockConfirmed": []
    }

    issued_timestamps = {
        "blockPreAccepted": [],
        "blockAccepted": [],
        "blockPreConfirmed": [],
        "blockConfirmed": []
    }

    # Read the file line by line
    with open(file_path, 'r') as file:
        for line in file:
            # Parse each line as JSON
            log_entry = json.loads(line)
            event_data = log_entry.get("event", {}).get("original", "{}")
            event_json = json.loads(event_data)

            # Extract the type
            entry_type = event_json.get("type")
            issued_timestamp = event_json.get("issuedTimestamp")

            # Based on the type, extract the specific time value
            if entry_type == "blockPreAccepted":
                time_value = event_json.get("preAcceptanceTime")
            elif entry_type == "blockAccepted":
                time_value = event_json.get("acceptanceTime")
            elif entry_type == "blockPreConfirmed":
                time_value = event_json.get("preConfirmedTime")
            elif entry_type == "blockConfirmed":
                time_value = event_json.get("confirmedTime")
            else:
                continue  # Skip if the type is not recognized

            # Append the time to the corresponding list in the dictionary, if it exists
            if time_value is not None:
                data[entry_type].append(time_value)
                issued_timestamps[entry_type].append(issued_timestamp)


    return data, issued_timestamps


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


def plot_data(data, issued_timestamps):
  
  # Adjusting the values from nanoseconds to seconds in the data
  data = {key: [value / 1e9 for value in val] for key, val in data.items()}

  # Find the minimum timestamp
  min_timestamp = min([min([parse_timestamp(ts) for ts in issued_timestamps[key]]) for key in issued_timestamps])

  # Filtering the data, chop the data which before the latest issuance time + 60s
  filtered_data = {}
  for key in issued_timestamps:
      times = [parse_timestamp(ts) for ts in issued_timestamps[key]]
      time_differences = [(t - min_timestamp).total_seconds() for t in times]
      filtered_data[key] = [value for time, value in zip(time_differences, data[key]) if time >= 60]

  # Preparing data for the violin plot
  types = []
  values = []

  for key, val in filtered_data.items():
      types.extend([key] * len(val))
      values.extend(val)

  # Create a DataFrame for Seaborn
  df = pd.DataFrame({'Type': types, 'Values': values})
  colors = [(0.4, 0.7607843137254902, 0.6470588235294118),
            (0.9882352941176471, 0.5529411764705883, 0.3843137254901961),
            (0.5529411764705883, 0.6274509803921569, 0.796078431372549),
            (0.9058823529411765, 0.5411764705882353, 0.7647058823529411)]

  # Plotting the violin plot
  plt.figure(figsize=(10, 6))
  sns.violinplot(x='Type', y='Values', data=df, palette='Set2', linewidth=2, width=0.8, cut=0)
  plt.xlabel('Type', fontname='Times New Roman', fontsize=18, fontweight='bold')
  plt.ylabel('Time (s)', fontname='Times New Roman', fontsize=18, fontweight='bold')

  
  plt.xticks(range(0, 4), ['Pre Accepted', 'Accepted', 'PreConfirmed', 'Confirmed'], fontname='Times New Roman', fontsize=16)
  plt.yticks(fontname='Times New Roman', fontsize=16)
  plt.tight_layout()
  plt.show()

  plt.figure(figsize=(10, 6))
  # Defining colors and markers for each key to distinguish them in the plot
  markers = ['o', 's', '^', 'd']
  line_styles = ['-', '--', '-.', ':']

  # Find the minimum timestamp across all keys
  min_timestamp = min(
      [min([datetime.fromisoformat(ts.replace('Z', '+00:00')) for ts in issued_timestamps[key]]) for key in issued_timestamps]
  )

  for idx, key in enumerate(issued_timestamps):
      # Convert string timestamps to datetime objects and subtract the minimum timestamp
      times = [(datetime.fromisoformat(ts.replace('Z', '+00:00')) - min_timestamp).total_seconds() for ts in issued_timestamps[key]]

      # Sort the data based on timestamps
      sorted_indices = np.argsort(times)
      sorted_times = np.array(times)[sorted_indices]
      sorted_values = np.array(data[key])[sorted_indices]

      # Plotting
      plt.plot(sorted_times, sorted_values, marker=markers[idx], linestyle=line_styles[idx], color=colors[idx], label=key)


  plt.xlabel('Simulation Time Since the First Issuance (s)', fontname='Times New Roman', fontsize=18, fontweight='bold')
  plt.xticks(fontname='Times New Roman', fontsize=16)
  plt.ylabel('Time (s)', fontname='Times New Roman', fontsize=18, fontweight='bold')
  plt.xlim(left=60)
  plt.yticks(fontname='Times New Roman', fontsize=16)

  plt.grid(True)
  plt.legend()
  plt.show()
  plt.cla()

if __name__ == "__main__":
  log_data, issued_timestamps = process_log_file('../../../profiling_results/2023_11_21_11_29/0.log')
  plot_data(log_data, issued_timestamps)
  
