import { spawnSync } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve, join, relative } from 'node:path';
import coverageLibrary from 'istanbul-lib-coverage';
import sourceMaps from 'istanbul-lib-source-maps';

const output = resolve(process.argv[2] || 'coverage');
await mkdir(output, { recursive: true });
const coverage = coverageLibrary.createCoverageMap({});
for (const suite of ['browser', 'item-editor', 'form-recovery']) {
  const report = join(output, suite + '.json');
  const result = spawnSync(process.execPath, ['test/' + suite + '.mjs'], {
    stdio: 'inherit',
    env: { ...process.env, BOBBY_BROWSER_COVERAGE: report, BOBBY_SCREENSHOTS: join(output, 'screenshots') },
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(suite + ' coverage suite failed');
  coverage.merge(JSON.parse(await readFile(report, 'utf8')));
}

const mapped = await sourceMaps.createSourceMapStore().transformCoverage(coverage);
const files = mapped.files().sort();
if (!files.some(file => file.endsWith('Chat.tsx'))) throw new Error('React source coverage is missing');
const results = Object.fromEntries(files.map(file => {
  const name = relative(process.cwd(), file).replaceAll('\\', '/');
  return [name, mapped.fileCoverageFor(file).toSummary().toJSON()];
}));
const total = mapped.getCoverageSummary().toJSON();
await writeFile(join(output, 'summary.json'), JSON.stringify({ total, files: results }, null, 2) + '\n');
for (const [file, result] of Object.entries(results)) console.log(file, result.lines.pct + '% lines');
console.log(`Total: ${total.lines.pct}% lines (${total.lines.covered}/${total.lines.total})`);
