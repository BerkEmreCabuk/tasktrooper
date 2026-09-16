import path from "path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const LOCAL_SERVER = "http://localhost:8186";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      // `__dirname` yerine `import.meta.dirname`: Vite 8'in native config
      // loader'ı (gelecek major'da varsayılan olacak) CJS değişkenlerini
      // desteklemiyor ve build sırasında uyarı basıyor.
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    port: 3200,
    strictPort: true,
    proxy: {
      "/v1": LOCAL_SERVER,
      "/admin": LOCAL_SERVER,
      "/health": LOCAL_SERVER,
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  base: "/",
});
