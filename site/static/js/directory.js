// The rows are server-rendered; this only filters them by toggling `hidden`.
(function () {
  var rows = Array.prototype.slice.call(document.querySelectorAll("#rows tr"));
  var q = document.getElementById("q");
  var region = document.getElementById("region");
  var cat = document.getElementById("cat");
  var curatedOnly = document.getElementById("curatedonly");
  var count = document.getElementById("count");

  var cats = {};
  rows.forEach(function (r) { if (r.dataset.c) cats[r.dataset.c] = true; });
  Object.keys(cats).sort().forEach(function (c) {
    var o = document.createElement("option");
    o.value = o.textContent = c;
    cat.appendChild(o);
  });

  function filter() {
    var term = q.value.trim().toLowerCase();
    var reg = region.value, ct = cat.value, cur = curatedOnly.checked;
    var shown = 0;
    rows.forEach(function (r) {
      var d = r.dataset;
      var hide = (term && d.n.indexOf(term) === -1) ||
        (reg && d.r !== reg) ||
        (ct && d.c !== ct) ||
        (cur && !("g" in d));
      r.hidden = !!hide;
      if (!hide) shown++;
    });
    count.textContent = shown + " of " + rows.length + " brokers";
  }

  [q, region, cat, curatedOnly].forEach(function (el) {
    el.addEventListener("input", filter);
    el.addEventListener("change", filter);
  });
})();
