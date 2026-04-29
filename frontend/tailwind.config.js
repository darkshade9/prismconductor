import typography from "@tailwindcss/typography";

/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        col: {
          todo: "#1e293b",
          plan: "#1e3a5f",
          inprog: "#3b2f5f",
          review: "#5f4a1e",
          done: "#1e5f3a",
        },
      },
    },
  },
  plugins: [typography],
};
