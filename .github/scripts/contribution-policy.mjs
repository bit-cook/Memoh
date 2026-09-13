import { createHash } from 'node:crypto';

export const noHumanQA = '⚠️ **No human QA** — this PR has not been verified by a human yet. Remove this line once a human confirms the happy path.';
export const headings = {
  author: 'Author', type: 'Type', summary: 'Summary', validation: 'Validation',
  screenshots: 'Screenshots / Recordings', qa: 'Human QA', bug: 'Bug Description', steps: 'Steps to Reproduce',
  expected: 'Expected and Actual Behavior', version: 'Version', feature: 'Feature Description', motivation: 'Use Case and Motivation',
  help: 'Problem', goal: 'Desired Outcome', attempts: 'What You Have Tried', environment: 'Version and Environment',
};
const sectionNames = new Set([...Object.values(headings), 'Related Issues', 'Environment', 'Model Used', 'Logs', 'Additional Context', 'Proposed Solution', 'Alternatives Considered']);

export const typeLabels = ['bug', 'feat', 'test', 'help'];
export const ciWorkflows = ['eslint.yml', 'go-ci.yml', 'rust-ci.yml', 'runtime-ci.yml', 'migrations.yml', 'install-ci.yml', 'electron-ci.yml', 'docker-pr.yml', 'contribution-policy-ci.yml'];

// 代码示例不参与字段或勾选解析；未知标题保留在所属字段内。
export function sections(body = '') {
  const result = new Map();
  let heading;
  let fence;
  for (const line of body.replace(/<!--[^]*?-->/g, '').split(/\r?\n/)) {
    const delimiter = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (fence) {
      if (delimiter && delimiter[1][0] === fence[0] && delimiter[1].length >= fence.length && !delimiter[2].trim()) fence = undefined;
      else if (heading) result.get(heading).content.push(line);
      continue;
    }
    if (delimiter) { fence = delimiter[1]; continue; }
    const match = line.match(/^#{2,3}\s+(.+?)\s*#*$/);
    if (match && sectionNames.has(match[1].trim())) {
      heading = match[1].trim();
      if (result.has(heading)) throw new Error(`Duplicate section: ${heading}`);
      result.set(heading, { content: [], plain: [], choices: [] });
    } else if (heading) {
      result.get(heading).content.push(line);
      result.get(heading).plain.push(line);
      const checked = line.match(/^\s*-\s+\[[xX]\]\s+(.+?)\s*$/);
      if (checked) result.get(heading).choices.push(checked[1]);
    }
  }
  return result;
}

export function validate(body, isPR) {
  const errors = [];
  let parts;
  try { parts = sections(body ?? ''); } catch (error) { return { errors: [error.message] }; }
  const content = key => parts.get(headings[key])?.content.join('\n').trim() ?? '';
  function required(key) {
    const value = content(key);
    if (!value || /^(?:_?No response_?|N\/?A|TBD|TODO|OK|done|passed|已完成|通过|Please fill in[.]?|无|待填写|请填写[。.]?|\.\.\.)$/i.test(value)) errors.push(`Please complete "${headings[key]}".`);
  }
  function choice(key, allowed) {
    const field = parts.get(headings[key]);
    const plain = field?.plain.join('\n').trim() ?? '';
    const values = isPR ? (field?.choices ?? []) : (plain ? [plain] : []);
    if (values.length !== 1 || !allowed.includes(values[0])) {
      errors.push(`Select exactly one option for "${headings[key]}": ${allowed.join(' / ')}.`);
      return undefined;
    }
    return values[0];
  }
  const author = choice('author', ['Human', 'Agent']);
  const type = choice('type', isPR ? ['bug', 'feat', 'test'] : ['bug', 'feat', 'help']);
  if (isPR) {
    ['summary', 'validation', 'screenshots', 'qa'].forEach(required);
    const qa = choice('qa', ['Not yet verified by a human', 'Confirmed by a human']);
    const disclosed = parts.get(headings.qa)?.plain.some(line => line.trim() === noHumanQA);
    if (qa === 'Not yet verified by a human' && !disclosed) errors.push('请在 Human QA 中保留 No human QA 声明，直到真人确认验收。');
    if (qa === 'Confirmed by a human') {
      const evidence = content('qa').replace(/^\s*-\s+\[[ xX]\].*$/gm, '').replace(noHumanQA, '').trim();
      if (!evidence || /^(?:TBD|TODO|待填写|N\/?A)$/i.test(evidence)) errors.push('Identify the reviewer and confirmation record in "Human QA".');
      if ((body ?? '').includes(noHumanQA)) errors.push('Remove the No human QA disclosure once a human has confirmed QA.');
    }
  } else {
    const fields = { bug: ['bug', 'steps', 'expected', 'version'], feat: ['feature', 'motivation'], help: ['help', 'goal', 'attempts', 'environment'] };
    (fields[type] ?? []).forEach(required);
    if (author === 'Agent') required('screenshots');
  }
  return { author, type, errors };
}

export function bodyFingerprint(pr) {
  return createHash('sha256').update(JSON.stringify([pr.head.sha, pr.base.ref, pr.body ?? ''])).digest('hex').slice(0, 24);
}

const handWrittenIcons = new Set(['Codex.vue', 'CodexColor.vue', 'Misskey.vue']);
export function excluded(path) {
  if (/^(?:pnpm-lock\.yaml|package-lock\.json|yarn\.lock|Cargo\.lock|go\.sum|skills-lock\.json)$/.test(path.split('/').at(-1))) return true;
  if (/^spec\/(docs\.go|swagger\.(json|yaml))$/.test(path) || /\.pb\.go$/.test(path)) return true;
  if (/^(packages\/sdk\/src\/|internal\/db\/postgres\/sqlc\/)/.test(path)) return true;
  if (path.startsWith('packages/icons/src/')) return !handWrittenIcons.has(path.slice('packages/icons/src/icons/'.length)) || !path.startsWith('packages/icons/src/icons/');
  return path === 'apps/web/src/components/file-manager/seti/vs-seti-icon-theme.json';
}
export function sizeLabel(additions, deletions) {
  const n = Math.max(additions, deletions);
  return `size:${n < 50 ? 'XS' : n < 500 ? 'S' : n < 1000 ? 'M' : n <= 3000 ? 'L' : 'XL'}`;
}
export function classify(files) {
  let additions = 0;
  let deletions = 0;
  let ignored = 0;
  const changes = new Set();
  const shared = /^(package\.json|pnpm-lock\.yaml|pnpm-workspace\.yaml|eslint\.config\.mjs|tsconfig\.json|vitest\.config\.ts)$/;
  for (const file of files) {
    const paths = [file.filename, file.previous_filename].filter(Boolean);
    // Count a rename into/out of generated output: only exclude when both sides are generated.
    if (paths.every(excluded)) ignored++;
    else { additions += file.additions; deletions += file.deletions; }
    for (const path of paths) {
      if (/^(apps\/web\/|packages\/(ui(?:\/|$)|icons\/|config\/|sdk\/)|patches\/)/.test(path) || shared.test(path)) changes.add('change:web');
      if (/^(apps\/desktop\/|packages\/config\/)/.test(path) || shared.test(path)) changes.add('change:desktop');
      if (/^db\/(?:[^/]+\/)*migrations\//.test(path)) changes.add('change:migrations');
      if (/^(cmd\/|internal\/|conf\/|db\/|spec\/)/.test(path) || /^(go\.(mod|sum)|sqlc\.yaml|openapi-ts\.config\.ts)$/.test(path)) changes.add('change:server');
    }
  }
  return { additions, deletions, ignored, size: sizeLabel(additions, deletions), changes: [...changes].sort() };
}
