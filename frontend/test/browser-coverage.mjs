import { readdir, writeFile } from 'node:fs/promises';
import { relative, resolve, sep } from 'node:path';
import coverageLibrary from 'istanbul-lib-coverage';
import instrumentLibrary from 'istanbul-lib-instrument';

const { createCoverageMap } = coverageLibrary;
const { createInstrumenter } = instrumentLibrary;

export function browserCoverage() {
  const output = process.env.BOBBY_BROWSER_COVERAGE;
  const files = new Map();
  const observed = createCoverageMap({});
  const sourceRoot = resolve('src') + sep;
  const isSource = path => path.startsWith(sourceRoot) && /(?<!\.d)\.tsx?$/.test(path);

  async function capture(page) {
    if (output) observed.merge(await page.evaluate(() => globalThis.__coverage__ || {}));
  }

  return {
    plugins: output ? [{
      name: 'bobby-browser-test-coverage',
      enforce: 'post',
      transform(code, id) {
        const file = resolve(id.split('?')[0]);
        if (!isSource(file)) return;
        const path = relative(process.cwd(), file).split(sep).join('/');
        const instrumenter = createInstrumenter({ esModules: true, produceSourceMap: true });
        const result = instrumenter.instrumentSync(code, path, this.getCombinedSourcemap());
        files.set(path, instrumenter.lastFileCoverage());
        return { code: result, map: instrumenter.lastSourceMap() };
      },
    }] : [],
    capture,
    async track(page) {
      if (!output) return;
      await page.exposeBinding('__saveBobbyCoverage', (_, coverage) => observed.merge(coverage));
      await page.addInitScript(() => {
        window.addEventListener('pagehide', () => {
          if (globalThis.__coverage__) void globalThis.__saveBobbyCoverage(globalThis.__coverage__);
        });
      });
    },
    async write(server) {
      if (!output) return;
      for (const file of await readdir('src', { recursive: true })) {
        if (isSource(resolve('src', file))) {
          await server.transformRequest('/src/' + file.split(sep).join('/'));
        }
      }
      const merged = createCoverageMap(Object.fromEntries(files));
      merged.merge(observed);
      await writeFile(output, JSON.stringify(merged.toJSON()));
    },
  };
}
