import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          "-apple-system",
          "BlinkMacSystemFont",
          "SF Pro Text",
          "Segoe UI Variable",
          "Segoe UI",
          "PingFang SC",
          "Microsoft YaHei UI",
          "sans-serif"
        ]
      },
      colors: {
        brand: "#6366F1"
      }
    }
  },
  plugins: []
} satisfies Config;
