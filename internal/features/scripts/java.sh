#!/bin/bash
set -euo pipefail
apt-get update && apt-get install -y openjdk-21-jdk-headless maven
curl -fsSL https://services.gradle.org/distributions/gradle-8.5-bin.zip -o /tmp/gradle.zip
unzip -q /tmp/gradle.zip -d /opt
ln -s /opt/gradle-8.5/bin/gradle /usr/local/bin/gradle
rm /tmp/gradle.zip
