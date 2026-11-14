import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const { BOBBY_API_URL = 'http://127.0.0.1:18000' } = loadEnv(mode, process.cwd(), 'BOBBY_');
  return {
    esbuild: { jsx: 'automatic' },
    server: {
      proxy: {
        '/v1': { target: BOBBY_API_URL, changeOrigin: true, ws: true, rewriteWsOrigin: true },
        '/session/bootstrap': { target: BOBBY_API_URL, changeOrigin: true, rewrite: () => '/' },
      },
    },
  };
});
