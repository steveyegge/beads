/**
 * theme.js - Theme switching and persistence for Beads UI
 *
 * Handles interactive theme selection via bead buttons in the header.
 * Updates the data-theme attribute on the html element to switch themes.
 * Updates bead selector UI to show the currently active theme.
 * Persists theme selection via localStorage (immediate) and server API (cookie).
 *
 * On page load, applies theme from localStorage if different from server-rendered theme,
 * ensuring user preference is applied before first paint.
 */

const THEME_SWITCHING_ENABLED = false;

export function initThemeSelector() {
  const beadButtons = document.querySelectorAll('[data-theme-selector]');
  const htmlElement = document.documentElement;

  if (beadButtons.length === 0) {
    console.warn('No theme selector beads found');
    return;
  }

  if (!THEME_SWITCHING_ENABLED) {
    beadButtons.forEach(button => {
      button.removeAttribute('disabled');
      button.disabled = false;
      button.removeAttribute('data-selected');
      button.setAttribute('aria-pressed', 'false');
      setTimeout(() => {
        button.setAttribute('aria-disabled', 'true');
        button.setAttribute('tabindex', '-1');
      }, 50);
      button.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
      });
    });

    try {
      localStorage.removeItem('beads-theme');
    } catch (error) {
      console.warn('Failed to clear stored theme preference:', error);
    }

    window.beadsTheme = {
      switch: (themeName) => {
        console.info(`Theme switching disabled; ignoring request for theme "${themeName || ''}"`);
        return htmlElement.getAttribute('data-theme') || 'white';
      },
      current: () => htmlElement.getAttribute('data-theme') || 'white'
    };

    return;
  }

  // Apply theme from localStorage if it differs from server-rendered theme
  // This ensures user preference is applied before first paint
  const applyStoredTheme = () => {
    try {
      const storedTheme = localStorage.getItem('beads-theme');
      const currentTheme = htmlElement.getAttribute('data-theme') || 'white';

      if (storedTheme && storedTheme !== currentTheme) {
        console.log(`Applying stored theme ${storedTheme} (server rendered ${currentTheme})`);
        htmlElement.setAttribute('data-theme', storedTheme);
        return storedTheme;
      }
      return currentTheme;
    } catch (error) {
      console.warn('Failed to read theme from localStorage:', error);
      return htmlElement.getAttribute('data-theme') || 'white';
    }
  };

  // Get current theme from html element
  const getCurrentTheme = () => {
    return htmlElement.getAttribute('data-theme') || 'white';
  };

  // Update UI to reflect current theme selection
  const updateBeadSelection = (themeName) => {
    beadButtons.forEach(button => {
      const buttonTheme = button.getAttribute('data-theme-selector');
      if (buttonTheme === themeName) {
        button.setAttribute('data-selected', 'true');
        button.setAttribute('aria-pressed', 'true');
      } else {
        button.removeAttribute('data-selected');
        button.setAttribute('aria-pressed', 'false');
      }
    });
  };

  // Switch to a new theme
  const switchTheme = (themeName) => {
    if (!themeName) {
      console.error('Theme name is required');
      return;
    }

    // Update html element's data-theme attribute
    htmlElement.setAttribute('data-theme', themeName);

    // Update bead button selection states
    updateBeadSelection(themeName);

    // Save to localStorage for immediate persistence
    try {
      localStorage.setItem('beads-theme', themeName);
    } catch (error) {
      console.warn('Failed to save theme to localStorage:', error);
    }

    // Persist to server (sets cookie for future page loads)
    saveThemeToServer(themeName);

    // Dispatch custom event for other modules to react to theme change
    window.dispatchEvent(new CustomEvent('beads:theme-changed', {
      detail: { theme: themeName }
    }));

    console.log(`Theme switched to: ${themeName}`);
  };

  // Save theme preference to server via API
  const saveThemeToServer = (themeName) => {
    fetch('/api/theme', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ theme: themeName })
    })
      .then(response => {
        if (!response.ok) {
          throw new Error(`Failed to save theme: HTTP ${response.status}`);
        }
        return response.json();
      })
      .then(data => {
        console.log('Theme preference saved to server:', data);
      })
      .catch(error => {
        console.warn('Failed to save theme preference to server:', error);
      });
  };

  // Initialize: apply stored theme first, then mark as selected
  const currentTheme = applyStoredTheme();
  updateBeadSelection(currentTheme);

  // Add click handlers to bead buttons
  beadButtons.forEach(button => {
    button.addEventListener('click', (e) => {
      e.preventDefault();

      if (button.disabled) {
        return;
      }

      const themeName = button.getAttribute('data-theme-selector');
      if (themeName) {
        switchTheme(themeName);
      }
    });

    // Initialize aria-pressed state
    const buttonTheme = button.getAttribute('data-theme-selector');
    button.setAttribute('aria-pressed', buttonTheme === currentTheme ? 'true' : 'false');
  });

  // Expose switchTheme for external use (e.g., from persistence module)
  window.beadsTheme = {
    switch: switchTheme,
    current: getCurrentTheme
  };
}

// Auto-initialize when module loads
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initThemeSelector);
} else {
  initThemeSelector();
}
