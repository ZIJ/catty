#!/usr/bin/env node

const https = require('https');
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const packageJson = require('../package.json');
const VERSION = process.env.CATTY_VERSION || packageJson.version;
const GITHUB_REPO = 'ZIJ/catty';

function getPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const platformMap = {
    darwin: 'darwin',
    linux: 'linux',
    win32: 'windows',
  };

  const archMap = {
    x64: 'amd64',
    arm64: 'arm64',
  };

  const os = platformMap[platform];
  const cpu = archMap[arch];

  if (!os || !cpu) {
    throw new Error(`Unsupported platform: ${platform}-${arch}`);
  }

  return { os, cpu, platform, arch };
}

function getBinaryName(os) {
  return os === 'windows' ? 'catty.exe' : 'catty';
}

function getDownloadUrl(os, cpu) {
  const ext = os === 'windows' ? '.exe' : '';
  return `https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/catty-${os}-${cpu}${ext}`;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);

    const request = (url) => {
      https.get(url, (response) => {
        // Handle redirects
        if (response.statusCode === 302 || response.statusCode === 301) {
          request(response.headers.location);
          return;
        }

        if (response.statusCode !== 200) {
          reject(new Error(`Failed to download: ${response.statusCode} ${response.statusMessage}`));
          return;
        }

        response.pipe(file);
        file.on('finish', () => {
          file.close(resolve);
        });
      }).on('error', (err) => {
        fs.unlink(dest, () => {});
        reject(err);
      });
    };

    request(url);
  });
}

async function install() {
  try {
    const { os, cpu, platform } = getPlatform();
    const binaryName = getBinaryName(os);
    const binDir = path.join(__dirname, '..', 'bin');
    const binaryPath = path.join(binDir, binaryName);

    // Create bin directory if it doesn't exist
    if (!fs.existsSync(binDir)) {
      fs.mkdirSync(binDir, { recursive: true });
    }

    // Check if binary already exists and is actually a binary (not placeholder)
    if (fs.existsSync(binaryPath)) {
      const stats = fs.statSync(binaryPath);
      // Placeholder is ~300 bytes, real binary is much larger
      if (stats.size > 1000) {
        console.log('catty binary already installed');
        return;
      }
      // Remove placeholder
      fs.unlinkSync(binaryPath);
    }

    const url = getDownloadUrl(os, cpu);
    console.log(`Downloading catty from ${url}...`);

    await download(url, binaryPath);

    // Make executable on Unix
    if (platform !== 'win32') {
      fs.chmodSync(binaryPath, 0o755);
    }

    console.log('catty installed successfully!');
  } catch (error) {
    console.error('Failed to install catty:', error.message);
    console.error('');
    console.error('You can manually download the binary from:');
    console.error(`https://github.com/${GITHUB_REPO}/releases`);
    process.exit(1);
  }
}

install();
