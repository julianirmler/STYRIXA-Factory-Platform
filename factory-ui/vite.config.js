import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Entwicklung: /api läuft über den Proxy zur lokalen factory-api (gleicher Ursprung, kein CORS).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: { "/api": "http://127.0.0.1:8090" },
  },
});
