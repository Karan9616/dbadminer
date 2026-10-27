(function () {
  "use strict";

  var state = {
    schema: "",
    table: "",
    limit: 50,
    offset: 0,
    order: "",
    dir: "asc"
  };

  function $(id) {
    return document.getElementById(id);
  }

  function cellText(value) {
    if (value === null || value === undefined) {
      return "null";
    }
    if (typeof value === "object") {
      return JSON.stringify(value);
    }
    return String(value);
  }

  function clear(node) {
    while (node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function api(path, options) {
    return fetch(path, options).then(function (res) {
      return res.text().then(function (text) {
        var body = null;
        if (text) {
          try {
            body = JSON.parse(text);
          } catch (err) {
            body = { error: text };
          }
        }
        if (!res.ok) {
          var msg = body && body.error ? body.error : res.statusText;
          throw new Error(msg);
        }
        return body;
      });
    });
  }

  function setStatus(msg) {
    $("status").textContent = msg || "";
  }

  function loadInfo() {
    return api("api/info").then(function (info) {
      var mode = info.access === "full" ? "full access" : "read-only";
      $("meta").textContent = info.dialect + " · " + mode + " · " + info.version;
    });
  }

  function loadTables() {
    return api("api/tables").then(function (data) {
      var list = $("tables");
      clear(list);
      (data.tables || []).forEach(function (table) {
        var li = document.createElement("li");
        var btn = document.createElement("button");
        btn.type = "button";
        btn.textContent = table.schema + "." + table.name;
        var kind = document.createElement("small");
        kind.textContent = " " + table.type;
        btn.appendChild(kind);
        btn.addEventListener("click", function () {
          selectTable(table.schema, table.name).catch(function (err) {
            setStatus(err.message);
          });
        });
        li.appendChild(btn);
        list.appendChild(li);
      });
    });
  }

  function selectTable(schema, table) {
    state.schema = schema;
    state.table = table;
    state.offset = 0;
    state.order = "";
    state.dir = "asc";
    $("table-title").textContent = schema + "." + table;
    return loadRows();
  }

  function rowsURL() {
    var params = new URLSearchParams();
    params.set("limit", String(state.limit));
    params.set("offset", String(state.offset));
    if (state.order) {
      params.set("order", state.order);
      params.set("dir", state.dir);
    }
    return "api/tables/" + encodeURIComponent(state.schema) + "/" + encodeURIComponent(state.table) + "/rows?" + params.toString();
  }

  function loadRows() {
    if (!state.table) {
      return Promise.resolve();
    }
    return api(rowsURL()).then(function (data) {
      renderGrid($("grid"), data, true);
      var from = data.rows.length ? state.offset + 1 : state.offset;
      $("page").textContent = from + "–" + (state.offset + data.rows.length);
      $("prev").disabled = state.offset === 0;
      $("next").disabled = !data.rows || data.rows.length < state.limit;
    });
  }

  function renderGrid(container, data, sortable) {
    clear(container);
    var table = document.createElement("table");
    var thead = document.createElement("thead");
    var hr = document.createElement("tr");
    (data.columns || []).forEach(function (name) {
      var th = document.createElement("th");
      if (sortable) {
        var btn = document.createElement("button");
        btn.type = "button";
        btn.textContent = name;
        btn.addEventListener("click", function () {
          sortBy(name);
        });
        th.appendChild(btn);
      } else {
        th.textContent = name;
      }
      hr.appendChild(th);
    });
    thead.appendChild(hr);
    var tbody = document.createElement("tbody");
    (data.rows || []).forEach(function (row) {
      var tr = document.createElement("tr");
      row.forEach(function (value) {
        var td = document.createElement("td");
        td.textContent = cellText(value);
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    });
    table.appendChild(thead);
    table.appendChild(tbody);
    container.appendChild(table);
  }

  function sortBy(name) {
    if (state.order === name) {
      state.dir = state.dir === "asc" ? "desc" : "asc";
    } else {
      state.order = name;
      state.dir = "asc";
    }
    state.offset = 0;
    loadRows().catch(function (err) {
      setStatus(err.message);
    });
  }

  function runSQL() {
    var sql = $("sql").value;
    return api("api/query", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql: sql })
    }).then(function (data) {
      if (data.columns) {
        renderGrid($("sql-result"), data, false);
      }
      if (typeof data.rowsAffected === "number") {
        setStatus(data.rowsAffected + " rows affected");
      } else {
        setStatus("");
      }
    });
  }

  $("prev").addEventListener("click", function () {
    state.offset = Math.max(0, state.offset - state.limit);
    loadRows().catch(function (err) {
      setStatus(err.message);
    });
  });
  $("next").addEventListener("click", function () {
    state.offset += state.limit;
    loadRows().catch(function (err) {
      setStatus(err.message);
    });
  });
  $("run").addEventListener("click", function () {
    runSQL().catch(function (err) {
      setStatus(err.message);
    });
  });

  loadInfo().then(loadTables).catch(function (err) {
    setStatus(err.message);
  });
})();
