// Flat ESLint config for the admin portal.
// eslint-config-next v16 ships native flat-config arrays, so we spread them
// directly (the FlatCompat shim used for v15 is no longer needed).
import coreWebVitals from "eslint-config-next/core-web-vitals";
import typescript from "eslint-config-next/typescript";

const eslintConfig = [
  ...coreWebVitals,
  ...typescript,
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts"],
  },
];

export default eslintConfig;
