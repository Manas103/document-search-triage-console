// Serves the production Angular build for the Playwright E2E run. No
// framework dev server in the loop: this is the same static bundle
// deploy/Dockerfile.frontend ships behind nginx.
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');

const root = path.resolve(__dirname, '..', 'dist', 'frontend', 'browser');
const port = Number(process.env.DSTC_STATIC_PORT || 4300);

const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.ico': 'image/x-icon' };

const server = http.createServer((req, res) => {
  let filePath = path.join(root, decodeURIComponent(req.url.split('?')[0]));
  if (!fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
    filePath = path.join(root, 'index.html');
  }
  const ext = path.extname(filePath);
  res.writeHead(200, { 'Content-Type': mime[ext] || 'application/octet-stream' });
  fs.createReadStream(filePath).pipe(res);
});

server.listen(port, () => {
  console.log(`static frontend server listening on :${port}, serving ${root}`);
});
