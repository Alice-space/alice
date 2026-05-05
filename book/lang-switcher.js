(function() {
  var lang = document.documentElement.lang || 'en';
  var other = lang === 'zh' ? 'en' : 'zh';
  var label = lang === 'zh' ? 'English' : '中文';
  var path = '../' + other + '/';

  // Try to match current page path in the other language
  var current = window.location.pathname;
  var mapped = current.replace('/' + lang + '/', '/' + other + '/');
  if (mapped !== current) path = mapped;

  // Inject into sidebar header
  var title = document.querySelector('.sidebar .chapter');
  if (title) {
    var link = document.createElement('a');
    link.href = path;
    link.style.cssText = 'display:block;padding:4px 16px;font-size:0.9em;color:var(--sidebar-fg);opacity:0.7';
    link.textContent = label;
    title.parentNode.insertBefore(link, title);
  }
})();
