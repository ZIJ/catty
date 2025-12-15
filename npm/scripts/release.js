#!/usr/bin/env node

const { execSync } = require('child_process');
const path = require('path');

const rootDir = path.join(__dirname, '..', '..');
const npmDir = path.join(__dirname, '..');

function run(cmd, cwd = rootDir) {
  console.log(`> ${cmd}`);
  execSync(cmd, { cwd, stdio: 'inherit' });
}

const versionType = process.argv[2] || 'patch';
if (!['patch', 'minor', 'major'].includes(versionType)) {
  console.error('Usage: node scripts/release.js [patch|minor|major]');
  process.exit(1);
}

// 1. Bump version
run(`npm version ${versionType} --no-git-tag-version`, npmDir);

// 2. Get new version
const packageJson = require('../package.json');
const version = packageJson.version;
console.log(`\nReleasing v${version}...\n`);

// 3. Build binaries (pass version to Makefile)
run(`make release VERSION=${version}`);

// 4. Create GitHub release
run(`gh release create v${version} dist/* --title "v${version}" --notes "Release v${version}"`);

// 5. Publish to npm
run('npm publish --access public', npmDir);

console.log(`\nReleased v${version} successfully!`);
