// Scrape https://developers.zalo.me/docs/social-api/tham-khao/ma-loi (JS-rendered SPA)
// into docs/zalo-error-codes.md as a markdown reference for the OA error catalog.
//
// Run on demand when Zalo updates the page. Not wired into CI/build.
//
// Usage: node scripts/fetch-zalo-error-codes.cjs

const { chromium } = require('/Users/vanducng/.nvm/versions/node/v22.21.1/lib/node_modules/@playwright/test/node_modules/playwright');
const fs = require('fs');
const path = require('path');

// The public docs site is a JS-rendered SPA; the underlying CDN serves the
// pre-rendered Docusaurus HTML which is far more scrape-friendly. Try the CDN
// first, fall back to the SPA only if the CDN path is missing.
// Multiple Zalo doc roots have an error-code page. We pull both Social API
// (user-facing) and Official Account (OA OpenAPI) since codes differ across
// surfaces. CDN paths render to static HTML; SPA URL is the fallback.
const TARGETS = [
  { name: 'social-api', url: 'https://stc-developers.zdn.vn/docs/v2/social-api/tham-khao/ma-loi?lang=vi' },
  { name: 'official-account', url: 'https://stc-developers.zdn.vn/docs/v2/official-account/tham-khao/ma-loi?lang=vi' },
  { name: 'official-account-api-ref', url: 'https://stc-developers.zdn.vn/docs/v2/official-account/api-tham-khao/ma-loi?lang=vi' },
  { name: 'bot-api', url: 'https://bot.zapps.me/docs/error-code/' },
];
const SPA_FALLBACK = 'https://developers.zalo.me/docs/social-api/tham-khao/ma-loi';
const OUT_FILE = path.join(__dirname, '..', 'docs', 'zalo-error-codes.md');

async function fetchPage(page, url, retries = 3) {
  for (let i = 0; i < retries; i++) {
    try {
      await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 });
      // Give React/lazy chunks more time on the first paint
      await page.waitForTimeout(8000);
      // Prefer real content selectors; fall back silently if none appear
      try {
        await page.waitForSelector('main h1, article h1, table, .doc-content', { timeout: 15000 });
      } catch (_) {
        // Selector wait failed, but the page may still have body text — continue
      }
      return true;
    } catch (err) {
      if (i === retries - 1) throw err;
      await page.waitForTimeout(2000 * (i + 1));
    }
  }
}

// Extract structured rows from any <table> on the page. Falls back to plain text
// if no table is found (Zalo sometimes renders codes as a flat list).
async function extract(page) {
  return page.evaluate(() => {
    const out = { tables: [], text: '' };

    document.querySelectorAll('table').forEach((tbl) => {
      const rows = [];
      tbl.querySelectorAll('tr').forEach((tr) => {
        const cells = [...tr.querySelectorAll('th,td')].map((c) =>
          (c.innerText || '').replace(/\s+/g, ' ').trim()
        );
        if (cells.length) rows.push(cells);
      });
      if (rows.length) out.tables.push(rows);
    });

    // Fallback: full body text minus boilerplate
    const text = (document.body.innerText || '')
      .split('\n')
      .filter((line) => {
        const l = line.trim().toLowerCase();
        return (
          l &&
          !l.includes('đăng nhập') &&
          !l.includes('cookie') &&
          !l.includes('từ chối') &&
          !l.includes('đồng ý') &&
          !l.includes('chọn ngôn ngữ') &&
          !l.match(/^anh$|^vn$/)
        );
      })
      .join('\n')
      .trim();

    out.text = text;
    return out;
  });
}

function tableToMarkdown(rows) {
  if (!rows.length) return '';
  const header = rows[0];
  const body = rows.slice(1);
  const escape = (s) => String(s).replace(/\|/g, '\\|');
  const head = `| ${header.map(escape).join(' | ')} |`;
  const sep = `| ${header.map(() => '---').join(' | ')} |`;
  const bodyMd = body.map((r) => `| ${r.map(escape).join(' | ')} |`).join('\n');
  return [head, sep, bodyMd].join('\n');
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  let md = '# Zalo Social API — Error Codes\n\n';
  md += `> Scraped: ${new Date().toISOString()}\n> Script: scripts/fetch-zalo-error-codes.cjs\n\n`;

  const sections = [];
  for (const target of TARGETS) {
    try {
      console.log(`Fetching ${target.name}: ${target.url} ...`);
      await fetchPage(page, target.url);
      const data = await extract(page);
      const hasTable = data.tables.length > 0;
      const hasMeaningfulText = data.text.length > 600 && /Mã lỗi|error code/i.test(data.text);
      console.log(`  → ${data.tables.length} table(s), ${data.text.length} chars, useful=${hasTable || hasMeaningfulText}`);
      if (hasTable || hasMeaningfulText) {
        sections.push({ target, data });
      } else {
        console.log('  (skipped: page is empty/redirect/SPA shell)');
      }
    } catch (err) {
      console.error(`  ✗ ${err.message}`);
    }
  }

  if (sections.length === 0) {
    try {
      console.log(`Falling back to SPA: ${SPA_FALLBACK} ...`);
      await fetchPage(page, SPA_FALLBACK);
      const data = await extract(page);
      if (data.tables.length > 0 || data.text.length > 500) {
        sections.push({ target: { name: 'spa-fallback', url: SPA_FALLBACK }, data });
      }
    } catch (err) {
      console.error(`  ✗ ${err.message}`);
    }
  }

  await browser.close();

  if (sections.length === 0) {
    md += '<!-- UNRENDERABLE: no candidate URL returned usable content -->\n';
  } else {
    for (const { target, data } of sections) {
      md += `## ${target.name}\n\n> Source: ${target.url}\n\n`;
      if (data.tables.length === 0) {
        md += '<!-- No <table> elements found. Raw page text below. -->\n\n```\n' + data.text + '\n```\n\n';
      } else {
        data.tables.forEach((rows, i) => {
          md += `### Table ${i + 1}\n\n${tableToMarkdown(rows)}\n\n`;
        });
        md += '<details><summary>Raw page text</summary>\n\n```\n' + data.text + '\n```\n\n</details>\n\n';
      }
      md += '---\n\n';
    }
  }

  fs.mkdirSync(path.dirname(OUT_FILE), { recursive: true });
  fs.writeFileSync(OUT_FILE, md, 'utf8');
  console.log(`✓ Wrote ${OUT_FILE}`);
})();
