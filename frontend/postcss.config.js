// Tailwind 4 split out the PostCSS plugin into its own package
// (@tailwindcss/postcss). The old `tailwindcss: {}` direct-plugin form
// errors at runtime starting with TW 4.
export default {
  plugins: {
    "@tailwindcss/postcss": {},
    autoprefixer: {},
  },
}
