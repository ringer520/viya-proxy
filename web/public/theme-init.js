// Apply the saved palette before first paint; otherwise a dark theme
      // flashes light while the bundle boots.
      (function () {
        try {
          var choice = localStorage.getItem("viya-theme") || "system";
          var palettes = ["light", "dark", "aurora", "ember", "cyber"];
          var systemDark = window.matchMedia("(prefers-color-scheme: dark)")
            .matches;
          var palette =
            choice === "system"
              ? systemDark
                ? "dark"
                : "light"
              : palettes.indexOf(choice) >= 0
                ? choice
                : "light";
          var root = document.documentElement;
          root.dataset.palette = palette;
          root.dataset.theme = palette === "light" ? "light" : "dark";
          root.dataset.density =
            localStorage.getItem("viya-compact-mode") === "true"
              ? "compact"
              : "comfortable";
        } catch (e) {}
      })();
