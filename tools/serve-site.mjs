/**
 * Serve docs/ for a local look at the project site.
 *
 *   node tools/serve-site.mjs          http://127.0.0.1:4173/
 *   node tools/serve-site.mjs 8080     somewhere else
 *
 * Two things it does that a stock static server does not, and both exist
 * because of a real failure rather than a preference.
 *
 * NO CACHING, at all. The screenshots and clips under docs/assets/ are
 * REGENERATED while this is running — that is the whole workflow — and a browser
 * holding the page open caches them by heuristic when no Cache-Control says
 * otherwise. Rewriting a file underneath a cached copy gave one reader two
 * images that would not decode in the page and opened perfectly in a new tab,
 * which is the confusing half of that failure: the bytes on disk were always
 * fine. `no-store` costs nothing on localhost and removes the class.
 *
 * The /SnmpLens/ PREFIX is stripped. The site has its own domain now and is
 * served from the root, so nothing produces those URLs any more — but GitHub
 * keeps answering the old wasabules.github.io/SnmpLens/ addresses and redirecting
 * them, so stale links exist in the wild and it costs one line to open them here
 * too.
 */
import { createServer } from 'node:http';
import { readFileSync, existsSync, statSync } from 'node:fs';
import { join, extname, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const docs = resolve(join(here, '..', 'docs'));
const port = parseInt(process.argv[2], 10) || 4173;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.xml': 'application/xml; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.webp': 'image/webp',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.svg': 'image/svg+xml',
  '.mp4': 'video/mp4',
  '.webm': 'video/webm',
  '.woff2': 'font/woff2',
  '.ico': 'image/x-icon',
};

const server = createServer((req, res) => {
  let path = decodeURIComponent(req.url.split('?')[0]);
  if (path.startsWith('/SnmpLens/')) path = path.slice('/SnmpLens'.length);
  if (path.endsWith('/')) path += 'index.html';

  const file = join(docs, path === '/' ? 'index.html' : path);

  // Never serve outside docs/, whatever the request says.
  if (!file.startsWith(docs) || !existsSync(file) || statSync(file).isDirectory()) {
    const notFound = join(docs, '404.html');
    res.writeHead(404, {
      'Content-Type': 'text/html; charset=utf-8',
      'Cache-Control': 'no-store',
    });
    res.end(existsSync(notFound) ? readFileSync(notFound) : 'not found');
    return;
  }

  const body = readFileSync(file);
  res.writeHead(200, {
    'Content-Type': MIME[extname(file)] || 'application/octet-stream',
    'Content-Length': body.length,
    'Cache-Control': 'no-store, must-revalidate',
  });
  res.end(body);
});

server.listen(port, '127.0.0.1', () => {
  console.log(`docs/ on http://127.0.0.1:${port}/`);
  console.log(`the demo on http://127.0.0.1:${port}/demo.html`);
  console.log('nothing is cached, so regenerating an image is enough to see it.');
});
