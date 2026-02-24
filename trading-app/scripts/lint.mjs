import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const targets = ['App.js', 'index.js', 'src'];
const jsFiles = [];
const skipDirs = new Set(['node_modules', '.expo', '.git', 'dist', 'build']);

function walk(entryPath) {
  const st = statSync(entryPath);
  if (st.isDirectory()) {
    const base = path.basename(entryPath);
    if (skipDirs.has(base)) return;
    for (const name of readdirSync(entryPath)) {
      walk(path.join(entryPath, name));
    }
    return;
  }
  if (!entryPath.endsWith('.js')) return;
  jsFiles.push(entryPath);
}

for (const target of targets) {
  const abs = path.join(root, target);
  try {
    walk(abs);
  } catch (_) {
    // Ignore optional paths.
  }
}

if (jsFiles.length === 0) {
  console.error('No JS files found to lint.');
  process.exit(1);
}

let hasError = false;

for (const file of jsFiles) {
  const src = readFileSync(file, 'utf8');
  const lines = src.split(/\r?\n/);
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line.startsWith('<<<<<<<') || line.startsWith('=======') || line.startsWith('>>>>>>>')) {
      console.error(`Merge marker found: ${path.relative(root, file)}:${i + 1}`);
      hasError = true;
    }
    if (/\s+$/.test(line)) {
      console.error(`Trailing spaces found: ${path.relative(root, file)}:${i + 1}`);
      hasError = true;
    }
  }
}

if (hasError) {
  process.exit(1);
}

console.log(`Lint passed (${jsFiles.length} files).`);
