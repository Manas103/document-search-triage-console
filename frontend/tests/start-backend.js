// Starts the real Go backend for the Playwright E2E run, against the same
// Elasticsearch instance the rest of this project's measurements use. On
// Windows (this project's primary dev machine) the backend is run inside
// WSL2, where the Go toolchain actually lives; on a native Linux CI runner
// it is run directly with `go run`, no WSL involved.
const { spawn } = require('node:child_process');
const path = require('node:path');

const backendDir = path.resolve(__dirname, '..', '..', 'backend');
const port = process.env.DSTC_TEST_PORT || '8081';
const esAddr = process.env.DSTC_TEST_ES_ADDR || 'http://localhost:9201';

function toWslPath(winPath) {
  return winPath.replace(/^([A-Za-z]):\\/, (_, d) => `/mnt/${d.toLowerCase()}/`).replace(/\\/g, '/');
}

let child;
if (process.platform === 'win32') {
  const wslPath = toWslPath(backendDir);
  child = spawn(
    'wsl.exe',
    ['-d', 'Ubuntu-22.04', '--cd', wslPath, '--', '/usr/local/go/bin/go', 'run', './cmd/server'],
    { env: { ...process.env, ES_ADDR: esAddr, PORT: port }, stdio: 'inherit' },
  );
} else {
  child = spawn('go', ['run', './cmd/server'], {
    cwd: backendDir,
    env: { ...process.env, ES_ADDR: esAddr, PORT: port },
    stdio: 'inherit',
  });
}

for (const sig of ['SIGTERM', 'SIGINT']) {
  process.on(sig, () => {
    child.kill();
    process.exit(0);
  });
}
