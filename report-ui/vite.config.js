import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    {
      name: 'serve-results-json',
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          if (req.url === '/results.json') {
            const resultsPath = path.resolve(__dirname, '../results.json');
            if (fs.existsSync(resultsPath)) {
              res.setHeader('Content-Type', 'application/json');
              res.end(fs.readFileSync(resultsPath));
              return;
            }
          }
          next();
        });
      }
    }
  ],
})
