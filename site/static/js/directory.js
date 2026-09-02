(function () {
  var src = document.currentScript.getAttribute("data-src") || "brokers.json";
  var rows = document.getElementById("rows");
  var q = document.getElementById("q");
  var region = document.getElementById("region");
  var cat = document.getElementById("cat");
  var curatedOnly = document.getElementById("curatedonly");
  var count = document.getElementById("count");
  var data = [];

  function esc(s) {
    return (s || "").replace(/[&<>"]/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
  }

  function render() {
    var term = q.value.trim().toLowerCase();
    var reg = region.value, ct = cat.value, cur = curatedOnly.checked;
    var out = [];
    for (var i = 0; i < data.length; i++) {
      var b = data[i];
      if (term && b.name.toLowerCase().indexOf(term) === -1) continue;
      if (reg && b.region !== reg) continue;
      if (ct && b.category !== ct) continue;
      if (cur && !b.curated) continue;
      var optout = b.opt_out_url
        ? '<a href="' + esc(b.opt_out_url) + '" rel="nofollow noopener">form</a>'
        : (b.email ? "email " + esc(b.email) : "—");
      var name = b.curated
        ? '<a href="' + encodeURIComponent(b.id) + '/">' + esc(b.name) + '</a> <span class="tag">guide</span>'
        : esc(b.name);
      out.push("<tr><td>" + name + "</td><td>" + esc(b.region) + "</td><td>" + esc(b.category) + "</td><td>" + optout + "</td></tr>");
    }
    rows.innerHTML = out.join("");
    count.textContent = out.length + " of " + data.length + " brokers";
  }

  fetch(src)
    .then(function (r) { return r.json(); })
    .then(function (json) {
      data = json;
      Array.from(new Set(data.map(function (b) { return b.category; }).filter(Boolean))).sort().forEach(function (c) {
        var o = document.createElement("option");
        o.value = o.textContent = c;
        cat.appendChild(o);
      });
      [q, region, cat, curatedOnly].forEach(function (el) {
        el.addEventListener("input", render);
        el.addEventListener("change", render);
      });
      render();
    })
    .catch(function () { count.textContent = "Could not load the broker list."; });
})();
