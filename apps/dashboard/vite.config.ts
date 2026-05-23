import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (id.includes("react-dom") || id.includes("react/")) return "react-vendor";
          if (
            id.includes("react-markdown") ||
            id.includes("remark-") ||
            id.includes("unified") ||
            id.includes("mdast-") ||
            id.includes("hast-") ||
            id.includes("micromark")
          ) {
            return "markdown";
          }
          if (
            id.includes("react-syntax-highlighter") ||
            id.includes("refractor") ||
            id.includes("prismjs")
          ) {
            return "syntax-highlight";
          }
        },
      },
    },
  },
  server: {
    port: 5174,
  },
});
