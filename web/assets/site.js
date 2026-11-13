/* Small enhancements for the static reading companion. */
(() => {
  'use strict';

  const root = document.documentElement;
  const themeButton = document.getElementById('theme-toggle');
  const themeLabel = document.getElementById('theme-label');
  const colorPreference = matchMedia('(prefers-color-scheme: dark)');
  let explicitTheme = false;
  try { explicitTheme = ['light', 'dark'].includes(localStorage.getItem('dictator-theme')); } catch (_) {}

  function updateThemeButton() {
    const target = root.dataset.theme === 'dark' ? 'light' : 'dark';
    themeButton.setAttribute('aria-label', `Switch to ${target} mode`);
    themeLabel.textContent = `${target === 'dark' ? 'Dark' : 'Light'} mode`;
  }
  themeButton.hidden = false;
  updateThemeButton();
  themeButton.addEventListener('click', () => {
    root.dataset.theme = root.dataset.theme === 'dark' ? 'light' : 'dark';
    explicitTheme = true;
    try { localStorage.setItem('dictator-theme', root.dataset.theme); } catch (_) {}
    updateThemeButton();
  });
  colorPreference.addEventListener('change', event => {
    if (!explicitTheme) {
      root.dataset.theme = event.matches ? 'dark' : 'light';
      updateThemeButton();
    }
  });

  const contents = document.getElementById('site-contents');
  const desktop = matchMedia('(min-width: 901px)');
  const navigation = document.getElementById('book-navigation');
  const filter = document.getElementById('chapter-filter');
  const filterStatus = document.getElementById('filter-status');
  const groups = [...navigation.querySelectorAll('.contents-group')];
  const chapterLinks = [...navigation.querySelectorAll('.chapter-list a')];
  let groupState;

  // The shell stays in place during HTMX navigation. Resolve its links once,
  // while their original page-relative context is still available.
  document.querySelectorAll('[data-persistent-link], #book-navigation a, link[rel="icon"]').forEach(link => {
    link.href = new URL(link.getAttribute('href'), location.href).href;
  });

  contents.open = desktop.matches;
  desktop.addEventListener('change', event => { contents.open = event.matches; });
  contents.querySelector('summary').addEventListener('click', event => {
    if (desktop.matches) event.preventDefault();
  });
  document.querySelector('.chapter-search').hidden = false;

  function currentPage() {
    return document.getElementById('main-content');
  }

  function updateNavigation() {
    const page = currentPage();
    navigation.querySelectorAll('a[data-page-id]').forEach(link => {
      const active = link.dataset.pageId === page.dataset.pageId;
      if (active) {
        link.setAttribute('aria-current', 'page');
        const group = link.closest('.contents-group');
        if (group) group.open = true;
      } else {
        link.removeAttribute('aria-current');
      }
    });
  }

  function filterChapters() {
    const query = filter.value.trim().toLocaleLowerCase();
    if (query && !groupState) groupState = groups.map(group => group.open);
    let count = 0;
    chapterLinks.forEach(link => {
      const text = `${link.dataset.search || ''} ${link.textContent}`.toLocaleLowerCase();
      const visible = !query || text.includes(query);
      link.closest('li').hidden = !visible;
      if (visible) count += 1;
    });
    groups.forEach((group, index) => {
      group.hidden = !!query && ![...group.querySelectorAll('.chapter-list li')].some(item => !item.hidden);
      if (query) group.open = !group.hidden;
      else if (groupState) group.open = groupState[index];
    });
    filterStatus.hidden = !query;
    filterStatus.textContent = query ? `${count} ${count === 1 ? 'chapter' : 'chapters'} found.` : '';
    if (!query) {
      groupState = undefined;
      updateNavigation();
    }
  }
  filter.addEventListener('input', filterChapters);
  filter.addEventListener('keydown', event => {
    if (event.key === 'Escape' && filter.value) {
      filter.value = '';
      filterChapters();
      event.preventDefault();
    }
  });

  function hashTarget() {
    if (!location.hash) return null;
    let id;
    try { id = decodeURIComponent(location.hash.slice(1)); } catch (_) { return null; }
    return document.getElementById(id);
  }

  function focusReading() {
    const main = currentPage();
    const target = hashTarget();
    main.focus({ preventScroll: true });
    (target || main).scrollIntoView({ block: 'start', behavior: 'instant' });
  }

  // HTMX must not intercept source files, downloads, or external navigation.
  // The builder marks these links too; this also protects later content additions.
  function prepareContent() {
    currentPage().querySelectorAll('a[href]').forEach(link => {
      const url = new URL(link.getAttribute('href'), location.href);
      if (url.origin !== location.origin || /\.(?:md|epub|kepub|json|tsv|pdf)$/i.test(url.pathname) || url.pathname.includes('/src/')) {
        link.setAttribute('hx-boost', 'false');
      }
    });
    updateNavigation();
  }

  document.addEventListener('htmx:afterSwap', event => {
    if (event.detail.target?.id !== 'main-content') return;
    prepareContent();
    if (!desktop.matches) contents.open = false;
  });
  document.addEventListener('htmx:afterSettle', event => {
    if (event.detail.target?.id !== 'main-content') return;
    focusReading();
    document.getElementById('navigation-announcement').textContent = currentPage().querySelector('h1').textContent;
  });
  document.addEventListener('htmx:historyRestore', () => {
    prepareContent();
    if (!desktop.matches) contents.open = false;
    currentPage().focus({ preventScroll: true });
  });
  prepareContent();
})();
