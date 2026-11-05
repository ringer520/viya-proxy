import js from "@eslint/js";
import tseslint from "typescript-eslint";
import pluginVue from "eslint-plugin-vue";
import prettier from "eslint-config-prettier";
import globals from "globals";

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "*.min.js"],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs["flat/recommended"],

  {
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
      },
    },
  },

  {
    // TypeScript 自己会检查未定义标识符（含 RequestInit 等 DOM 类型），
    // 基础规则 no-undef 不认识类型位置，故对 TS/Vue 关闭。
    files: ["**/*.{ts,vue}"],
    rules: {
      "no-undef": "off",
    },
  },

  {
    files: ["**/*.vue"],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        extraFileExtensions: [".vue"],
        sourceType: "module",
      },
    },
  },

  {
    rules: {
      // 组件名多为单页业务命名（Ddnspage、Layer4Page 等），不强制多词
      "vue/multi-word-component-names": "off",
      // 事件回调里允许使用空 catch 做静默降级
      "no-empty": ["error", { "allowEmptyCatch": true }],
      // 版式交给 Prettier，避免与格式化规则互相打架
      "vue/max-attributes-per-line": "off",
      "vue/singleline-html-element-content-newline": "off",
      "vue/multiline-html-element-content-newline": "off",
      "vue/html-self-closing": "off",
      "vue/attributes-order": "off",
      "vue/html-indent": "off",
      "vue/html-closing-bracket-newline": "off",
      // 与后端交互处存在少量 any，先降级为提示，逐步收敛
      "@typescript-eslint/no-explicit-any": "warn",
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrors: "none" },
      ],
    },
  },

  // 必须放最后：关闭所有与 Prettier 冲突的格式规则
  prettier,
);
