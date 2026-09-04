/**
 * A tokeniser for SMIv2, the language MIBs are written in.
 *
 * This exists instead of a code-editor library. CodeMirror would bring
 * selection, undo, search and virtualisation — but this application ships as
 * one offline binary with almost no frontend dependencies, and none of what it
 * would bring answers the question a person actually has in front of a MIB,
 * which is "why will this not load". That answer comes from the Go validator.
 * Colour is here to make the file readable while they fix it.
 *
 * The technique is a transparent <textarea> over a <pre> holding this markup,
 * kept in scroll sync. Its one hard rule: the two layers must lay out
 * identically, so the markup must contain exactly the same characters as the
 * text — no added spaces, no collapsed runs, no reordering.
 */

// The SMIv2 macro keywords: what a definition IS.
const MACROS = new Set([
  'MODULE-IDENTITY', 'OBJECT-IDENTITY', 'OBJECT-TYPE', 'NOTIFICATION-TYPE',
  'TEXTUAL-CONVENTION', 'OBJECT-GROUP', 'NOTIFICATION-GROUP', 'MODULE-COMPLIANCE',
  'AGENT-CAPABILITIES', 'TRAP-TYPE',
]);

// Clause keywords: the fields inside a definition.
const CLAUSES = new Set([
  'DEFINITIONS', 'BEGIN', 'END', 'IMPORTS', 'EXPORTS', 'FROM',
  'SYNTAX', 'UNITS', 'MAX-ACCESS', 'ACCESS', 'MIN-ACCESS', 'STATUS',
  'DESCRIPTION', 'REFERENCE', 'INDEX', 'AUGMENTS', 'IMPLIED', 'DEFVAL',
  'LAST-UPDATED', 'ORGANIZATION', 'CONTACT-INFO', 'REVISION',
  'OBJECTS', 'NOTIFICATIONS', 'OBJECT', 'MODULE', 'MANDATORY-GROUPS',
  'GROUP', 'WRITE-SYNTAX', 'PRODUCT-RELEASE', 'SUPPORTS', 'VARIABLES',
  'IDENTIFIER', 'SEQUENCE', 'OF', 'CHOICE', 'SIZE',
]);

// Base and application types.
const TYPES = new Set([
  'INTEGER', 'Integer32', 'Unsigned32', 'Counter32', 'Counter64', 'Gauge32',
  'TimeTicks', 'OCTET', 'STRING', 'OBJECT', 'NULL', 'BITS', 'IpAddress',
  'Opaque', 'DisplayString', 'TruthValue', 'RowStatus', 'PhysAddress',
  'MacAddress', 'TimeStamp', 'TimeInterval', 'DateAndTime', 'AutonomousType',
  'TestAndIncr', 'RowPointer', 'StorageType', 'TDomain', 'TAddress',
]);

// The closed vocabularies of MAX-ACCESS and STATUS. Highlighting these makes a
// typo like "read-onl" visibly not a keyword.
const VALUES = new Set([
  'read-only', 'read-write', 'read-create', 'not-accessible',
  'accessible-for-notify', 'write-only',
  'current', 'deprecated', 'obsolete', 'mandatory', 'optional',
  'true', 'false',
]);

const ESCAPES = { '&': '&amp;', '<': '&lt;', '>': '&gt;' };

function escapeHtml(s) {
  return s.replace(/[&<>]/g, (c) => ESCAPES[c]);
}

/**
 * Classify one bare word.
 * @param {string} word
 * @returns {string|null} a token class, or null to leave it plain
 */
function classifyWord(word) {
  if (MACROS.has(word)) return 'mac';
  if (CLAUSES.has(word)) return 'kw';
  if (TYPES.has(word)) return 'ty';
  if (VALUES.has(word)) return 'val';
  if (/^\d+$/.test(word)) return 'num';
  return null;
}

/**
 * Whether position `index` sits inside a quoted string.
 *
 * This is what lets the editor colour only the visible window. Highlighting
 * carries `inString` across lines — a DESCRIPTION spans many — so starting
 * mid-file needs to know the state at that point, and getting it wrong repaints
 * half the file as a string. Scanning for it costs a fraction of tokenising,
 * because it produces nothing: no markup, no escaping, no allocation per token.
 *
 * @param {string} text
 * @param {number} index character offset
 * @returns {boolean}
 */
export function stringStateAt(text, index) {
  const upto = Math.max(0, Math.min(index, text.length));
  let inString = false;
  let i = 0;

  while (i < upto) {
    const ch = text[i];
    if (inString) {
      if (ch === '"') inString = false;
      i++;
      continue;
    }
    if (ch === '"') {
      inString = true;
      i++;
      continue;
    }
    // A comment runs to the next `--` or to end of line, and a quote inside it
    // opens nothing.
    if (ch === '-' && text[i + 1] === '-') {
      let j = i + 2;
      while (j < text.length && text[j] !== '\n') {
        if (text[j] === '-' && text[j + 1] === '-') { j += 2; break; }
        j++;
      }
      i = j;
      continue;
    }
    // A string never spans a blank line boundary in practice, but the
    // tokeniser resets nothing at newlines, so neither does this.
    i++;
  }
  return inString;
}

/**
 * Turn MIB source into highlighted HTML, one line at a time.
 *
 * Comments and strings are handled by scanning rather than by regex over the
 * whole file: in SMI a comment runs from `--` to the next `--` or to the end of
 * the line, and a DESCRIPTION string spans many lines. A regex that ignored
 * that would colour half a file as a string the moment someone typed a quote.
 *
 * @param {string} text
 * @returns {string} HTML whose text content is character-identical to `text`
 */
export function highlight(text, startInString = false) {
  let out = '';
  let inString = startInString;

  const lines = String(text ?? '').split('\n');

  for (let l = 0; l < lines.length; l++) {
    const line = lines[l];
    let i = 0;

    while (i < line.length) {
      // A multi-line DESCRIPTION string swallows everything until its closing
      // quote, including what would otherwise look like keywords.
      if (inString) {
        const end = line.indexOf('"', i);
        if (end === -1) {
          out += `<span class="str">${escapeHtml(line.slice(i))}</span>`;
          i = line.length;
        } else {
          out += `<span class="str">${escapeHtml(line.slice(i, end + 1))}</span>`;
          i = end + 1;
          inString = false;
        }
        continue;
      }

      const ch = line[i];

      if (ch === '"') {
        inString = true;
        out += '<span class="str">&quot;</span>';
        i++;
        continue;
      }

      // `--` opens a comment that ends at the next `--` or at end of line.
      if (ch === '-' && line[i + 1] === '-') {
        const close = line.indexOf('--', i + 2);
        const end = close === -1 ? line.length : close + 2;
        out += `<span class="com">${escapeHtml(line.slice(i, end))}</span>`;
        i = end;
        continue;
      }

      if (/[A-Za-z0-9]/.test(ch)) {
        let j = i;
        while (j < line.length && /[A-Za-z0-9_-]/.test(line[j])) j++;
        const word = line.slice(i, j);
        const cls = classifyWord(word);
        out += cls ? `<span class="${cls}">${escapeHtml(word)}</span>` : escapeHtml(word);
        i = j;
        continue;
      }

      if (ch === ':' && line.slice(i, i + 3) === '::=') {
        out += '<span class="op">::=</span>';
        i += 3;
        continue;
      }

      if ('{}(),;|'.includes(ch)) {
        out += `<span class="pun">${escapeHtml(ch)}</span>`;
        i++;
        continue;
      }

      out += escapeHtml(ch);
      i++;
    }

    // Keep the newline: the mirror must have the same line count as the
    // textarea or every line below drifts out of alignment.
    if (l < lines.length - 1) out += '\n';
  }

  return out;
}

/** Snippets: the skeletons nobody remembers the exact shape of. */
export const SNIPPETS = [
  {
    key: 'objectType',
    text: `${'${name}'} OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION
        "Describe what this object reports."
    ::= { parent 1 }
`,
  },
  {
    key: 'moduleIdentity',
    text: `${'${name}'} MODULE-IDENTITY
    LAST-UPDATED "202601010000Z"
    ORGANIZATION "Your organisation"
    CONTACT-INFO
        "Who to contact."
    DESCRIPTION
        "What this module covers."
    ::= { enterprises 99999 }
`,
  },
  {
    key: 'textualConvention',
    text: `${'${name}'} ::= TEXTUAL-CONVENTION
    STATUS      current
    DESCRIPTION
        "What this type means."
    SYNTAX      Integer32
`,
  },
  {
    key: 'table',
    // A table that LOADS.
    //
    // This used to emit the table and its row and nothing else: no SEQUENCE
    // type, so `SYNTAX <name>Entry` referred to nothing; no columns; and an
    // INDEX naming an object that did not exist. Inserting it produced a MIB
    // that cannot resolve, which is the opposite of what a snippet is for.
    // The row's SYNTAX also has to be the CAPITALISED type name — SMI type
    // references start with an upper-case letter.
    text: `${'${name}'}Table OBJECT-TYPE
    SYNTAX      SEQUENCE OF ${'${Name}'}Entry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION
        "What this table lists."
    ::= { parent 1 }

${'${name}'}Entry OBJECT-TYPE
    SYNTAX      ${'${Name}'}Entry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION
        "One row."
    INDEX   { ${'${name}'}Index }
    ::= { ${'${name}'}Table 1 }

${'${Name}'}Entry ::= SEQUENCE {
    ${'${name}'}Index    Integer32,
    ${'${name}'}Descr    DisplayString
}

${'${name}'}Index OBJECT-TYPE
    SYNTAX      Integer32 (1..2147483647)
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION
        "Identifies a row."
    ::= { ${'${name}'}Entry 1 }

${'${name}'}Descr OBJECT-TYPE
    SYNTAX      DisplayString
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION
        "What this row is."
    ::= { ${'${name}'}Entry 2 }
`,
  },
  {
    key: 'imports',
    text: `IMPORTS
    MODULE-IDENTITY, OBJECT-TYPE, Integer32, Counter32
        FROM SNMPv2-SMI
    DisplayString, TEXTUAL-CONVENTION
        FROM SNMPv2-TC;
`,
  },
  {
    key: 'module',
    text: `${'${name}'} DEFINITIONS ::= BEGIN

IMPORTS
    MODULE-IDENTITY, OBJECT-TYPE
        FROM SNMPv2-SMI;

END
`,
  },
];
