// Ambient declarations for non-code side-effect imports.
// TypeScript 6 (TS2882) rejects side-effect imports such as `import "./globals.css"`
// unless the module is declared. Next.js resolves these via its bundler at build
// time; this declaration just satisfies the type checker.
declare module "*.css";
