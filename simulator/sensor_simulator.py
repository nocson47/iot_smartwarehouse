#!/usr/bin/env python3
"""
Simple MQTT sensor simulator using paho-mqtt.

Usage:
  pip install paho-mqtt
  MQTT_BROKER=localhost:1883 python simulator/sensor_simulator.py

This publishes JSON messages to 'sensors/sim-<id>' periodically.
"""
import os
import time
import json
import random
import argparse

import paho.mqtt.publish as publish

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--broker', default=os.getenv('MQTT_BROKER', 'localhost:1883'))
    parser.add_argument('--topic', default='sensors/sim-01')
    parser.add_argument('--interval', type=float, default=2.0)
    args = parser.parse_args()

    host, port = args.broker.split(':') if ':' in args.broker else (args.broker, 1883)
    port = int(port)

    print(f"Publishing to {host}:{port} -> {args.topic}")
    while True:
        payload = {
            'device': args.topic.split('/')[-1],
            'temp': round(20 + random.random() * 10, 2),
            'hum': round(40 + random.random() * 20, 2),
            'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
        }
        try:
            publish.single(args.topic, json.dumps(payload), hostname=host, port=port)
        except Exception as e:
            print('publish error', e)
        time.sleep(args.interval)

if __name__ == '__main__':
    main()
