// The syntax highlighter's load-bearing invariant.
//
// The editor is a transparent <textarea> over a <pre> holding highlighted
// markup. The two layers only stay aligned if the markup renders exactly the
// same characters as the text — one added space, one collapsed run, one lost
// newline, and every line below it drifts. That is invisible in a screenshot of
// line 1 and maddening on line 400, so it is checked here rather than by eye.
import { highlight, SNIPPETS } from '../src/mibeditor/tokenize.js';

let failures = 0;
const check = (name, ok, extra = '') => {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? ' — ' + extra : ''}`);
  if (!ok) failures++;
};

/** Recover the text a browser would render from the generated markup. */
function textOf(html) {
  return html
    .replace(/<[^>]+>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, '&');
}

const SAMPLES = {
  'a whole module': `TEST-MIB DEFINITIONS ::= BEGIN

IMPORTS
    MODULE-IDENTITY, OBJECT-TYPE, Counter32
        FROM SNMPv2-SMI;

ifInOctets OBJECT-TYPE
    SYNTAX      Counter32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION
        "The total number of octets received."
    ::= { ifEntry 10 }

END
`,
  'comments': '-- a comment --\nfoo -- trailing\nbar --x-- baz\n',
  'a multi-line string': 'DESCRIPTION\n    "line one\n     line two with -- not a comment\n     and OBJECT-TYPE which is not a keyword"\n',
  'markup characters': 'a < b & c > d "quoted" <span>\n',
  'tabs and trailing spaces': '\tindented   \n  \t mixed\t\n',
  'empty': '',
  'only newlines': '\n\n\n',
  'no trailing newline': 'END',
  'unicode': 'DESCRIPTION "Seuil dépassé — 数値が高すぎる"\n',
  'an unterminated string': 'DESCRIPTION "never closed\nSTATUS current\n',
};

for (const [name, src] of Object.entries(SAMPLES)) {
  const html = highlight(src);
  const back = textOf(html);
  check(`round trip: ${name}`, back === src,
    back === src ? '' : `got ${JSON.stringify(back.slice(0, 60))} want ${JSON.stringify(src.slice(0, 60))}`);
}

// Line count must match exactly, or the gutter numbers point at the wrong rows.
for (const [name, src] of Object.entries(SAMPLES)) {
  const lines = (s) => s.split('\n').length;
  check(`line count: ${name}`, lines(textOf(highlight(src))) === lines(src));
}

// Markup must be escaped, or a MIB containing "<" would inject tags into the
// mirror layer.
const injected = highlight('DESCRIPTION "<img src=x onerror=alert(1)>"\n');
check('markup in the source is escaped',
  !injected.includes('<img') && injected.includes('&lt;img'));

// Comments must win over keywords: a commented-out OBJECT-TYPE is not a
// definition.
const commented = highlight('-- OBJECT-TYPE is mentioned here\n');
check('a keyword inside a comment is not highlighted as one',
  commented.includes('class="com"') && !commented.includes('class="mac"'));

// A string must win too, or the first quote in a DESCRIPTION would repaint the
// rest of the file.
const stringy = highlight('"SYNTAX Counter32 STATUS current"\n');
check('keywords inside a string are not highlighted',
  !stringy.includes('class="kw"') && !stringy.includes('class="ty"'));

// The categories that actually help: a macro, a clause, a type, a closed value.
const real = highlight('x OBJECT-TYPE\n SYNTAX Counter32\n MAX-ACCESS read-only\n');
for (const [label, cls] of [['macro', 'mac'], ['clause', 'kw'], ['type', 'ty'], ['enum value', 'val']]) {
  check(`${label} is classified`, real.includes(`class="${cls}"`));
}

// A typo must NOT be classified — that is the whole point of colouring a
// closed vocabulary.
const typo = highlight(' MAX-ACCESS read-onl\n');
check('a mistyped access value is left plain', !typo.includes('>read-onl<'));

// Performance: the largest bundled MIB is ~186 KB. Highlighting must not take
// so long that typing stutters.
const big = SAMPLES['a whole module'].repeat(600); // ~200 KB
const started = process.hrtime.bigint();
const bigHtml = highlight(big);
const ms = Number(process.hrtime.bigint() - started) / 1e6;
check('a 200 KB MIB highlights fast enough to type over', ms < 400, `${ms.toFixed(0)} ms`);
check('the big file round trips too', textOf(bigHtml) === big);

// Snippets must be usable: every one has a key and a placeholder.
check('every snippet has a key and body',
  SNIPPETS.length > 0 && SNIPPETS.every((s) => s.key && s.text));
check('snippets carry a name placeholder',
  SNIPPETS.filter((s) => s.text.includes('${name}')).length >= 4);

process.exit(failures ? 1 : 0);
