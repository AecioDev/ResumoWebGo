(() => {
  const MESES = [
    "Janeiro", "Fevereiro", "Março", "Abril", "Maio", "Junho",
    "Julho", "Agosto", "Setembro", "Outubro", "Novembro", "Dezembro"
  ];

  const state = {
    serverMode: false,
    loggedIn: false,
    user: null, // { id, username, nome, role, empresaIds, socioNome }
    auth: null, // Drive status
    empresas: [],
    users: [],
    selectedEmpresaId: "",
    tipo: "S",
    ano: new Date().getFullYear(),
    anoAtualSistema: new Date().getFullYear(),
    mes: new Date().getMonth() + 1,
    activeTabIndex: 0,
    resumoSocios: null,
    resumoAcerto: null,
    sortCol: "socio",
    sortAsc: true,
    searchQuery: "",
    pickerTarget: null,
    pickerShared: false,
    pickerStack: [],
    formPastasPorAno: {},
    formAnoAtivo: String(new Date().getFullYear()),
  };

  const fmtBRL = new Intl.NumberFormat("pt-BR", { style: "currency", currency: "BRL" });
  const fmtPct = new Intl.NumberFormat("pt-BR", { style: "percent", minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const fmtInt = new Intl.NumberFormat("pt-BR", { maximumFractionDigits: 0 });
  const $ = (id) => document.getElementById(id);

  const isAdmin = () => state.user && state.user.role === "admin";
  const isGestor = () => state.user && state.user.role === "gestor";
  const isSocio = () => state.user && state.user.role === "socio";

  function kpiSignClass(val) {
    if (val < -0.005) return "negative";
    if (val > 0.005) return "positive";
    return "";
  }

  function cellSignClass(val) {
    if (val < -0.005) return "val-neg";
    if (val > 0.005) return "val-pos";
    return "";
  }

  function roleLabel(role) {
    if (role === "admin") return "Admin";
    if (role === "gestor") return "Gestor";
    return "Sócio";
  }

  function showAlert(msg, type = "error") {
    const box = $("alertBox");
    if (!msg) {
      box.classList.add("hidden");
      return;
    }
    box.textContent = msg;
    box.className = `alert alert-${type} no-print`;
  }

  async function api(url, options = {}) {
    const resp = await fetch(url, options);
    const data = await resp.json().catch(() => ({}));
    if (resp.status === 401 && state.serverMode && url !== "/api/login") {
      state.loggedIn = false;
      state.user = null;
      renderShellVisibility();
      throw new Error(data.error || "Sessão expirada. Faça login novamente.");
    }
    if (!resp.ok) {
      throw new Error(data.error || `Erro HTTP ${resp.status}`);
    }
    return data;
  }

  function modoLabel(modo) {
    if (modo === "S") return "Sócios";
    if (modo === "A") return "Acerto";
    return "Ambos";
  }

  function getSelectedEmpresa() {
    return state.empresas.find((e) => e.id === state.selectedEmpresaId) || null;
  }

  function getEmpresaAnos(emp) {
    if (!emp || !emp.pastasPorAno) return [state.anoAtualSistema];
    const years = Object.keys(emp.pastasPorAno)
      .map((y) => parseInt(y, 10))
      .filter((y) => !isNaN(y) && y >= 2000 && y <= 2100)
      .sort((a, b) => b - a);
    return years.length > 0 ? years : [state.anoAtualSistema];
  }

  async function bootstrap() {
    const params = new URLSearchParams(window.location.search);
    const urlAuthError = params.get("authError");
    const urlAuthOk = params.get("authOk");
    const urlEmpId = params.get("empresaId") || "";
    const urlAno = parseInt(params.get("ano") || "", 10);
    const urlTipo = (params.get("tipo") || "").toUpperCase();
    const urlMes = parseInt(params.get("mes") || "", 10);

    if (urlAuthError || urlAuthOk || urlEmpId || !isNaN(urlAno) || urlTipo || !isNaN(urlMes)) {
      window.history.replaceState({}, "", "/");
    }

    const status = await api("/api/status");
    state.serverMode = !!status.serverMode;
    state.loggedIn = !!status.loggedIn;
    state.user = status.user || null;
    state.auth = status.auth || null;
    state.empresas = status.empresas || [];
    state.anoAtualSistema = status.anoAtual || new Date().getFullYear();
    state.ano = (!isNaN(urlAno) && urlAno >= 2000 && urlAno <= 2100) ? urlAno : state.anoAtualSistema;
    state.mes = (!isNaN(urlMes) && urlMes >= 1 && urlMes <= 12) ? urlMes : (status.mesAtual || 1);
    if (urlTipo === "S" || urlTipo === "A") {
      state.tipo = urlTipo;
    }

    renderShellVisibility();
    if (!state.loggedIn) {
      if (urlAuthError) {
        $("loginError").textContent = urlAuthError;
        $("loginError").classList.remove("hidden");
      }
      return;
    }

    if (urlAuthError) {
      showAlert(urlAuthError, "error");
    } else if (urlAuthOk) {
      showAlert("Google Drive conectado com sucesso!", "success");
    }

    if (status.redirectUri) $("redirectUriLabel").textContent = status.redirectUri;
    if (status.dataDir) $("dataDirLabel").textContent = status.dataDir;

    if (urlEmpId && state.empresas.some((e) => e.id === urlEmpId)) {
      state.selectedEmpresaId = urlEmpId;
    }

    renderRoleUI();
    renderDriveAuthState();
    populateMesSelect();
    renderEmpresasSelect();

    if (state.auth && state.auth.authenticated && state.empresas.length > 0) {
      await onEmpresaChanged(true);
    }
  }

  function renderShellVisibility() {
    $("loginScreen").classList.toggle("hidden", state.loggedIn);
    $("appShell").classList.toggle("hidden", !state.loggedIn);
  }

  function renderRoleUI() {
    const admin = isAdmin();
    const gestor = isGestor();
    const socio = isSocio();

    $("btnConfig").classList.toggle("hidden", !admin);
    $("btnClearCache").classList.toggle("hidden", !admin);
    $("authBadge").classList.toggle("hidden", !admin);
    $("btnNovaEmpresaEmpty").classList.toggle("hidden", !admin);

    // Botão Usuários aparece no modo servidor para Admin e Gestor
    const showUsersBtn = state.serverMode && (admin || gestor);
    $("btnUsers").classList.toggle("hidden", !showUsersBtn);
    $("btnUsers").textContent = gestor ? "👥 Acessos dos Sócios" : "👥 Usuários";

    $("btnAppLogout").classList.toggle("hidden", !state.serverMode);
    $("btnShutdown").classList.toggle("hidden", state.serverMode);

    if (state.serverMode && state.user) {
      $("userBadge").classList.remove("hidden");
      $("userBadge").textContent = socio
        ? `👤 ${state.user.socioNome} (Sócio)`
        : `👤 ${state.user.nome || state.user.username} (${roleLabel(state.user.role)})`;
    } else {
      $("userBadge").classList.add("hidden");
    }

    $("topbarSubtitle").textContent = socio
      ? `Extrato individual — ${state.user.socioNome}`
      : "Leitura direta do Google Drive • Sem vínculos quebrados";

    $("searchBoxWrap").classList.toggle("hidden", socio);
    $("noEmpresaMsg").textContent = admin
      ? "Cadastre sua primeira empresa, escolha se ela usa Sócios, Acerto Semanal ou Ambos e selecione as pastas no Google Drive."
      : "Nenhuma empresa foi vinculada ao seu usuário ainda. Solicite ao administrador.";
  }

  function renderDriveAuthState() {
    const badge = $("authBadge");
    const setup = $("setupSection");
    const notReady = $("driveNotReadyCard");
    const dash = $("dashboardSection");

    if (state.auth && state.auth.authenticated) {
      const modeTag = state.auth.authMode === "service_account" ? "Service Account" : (state.auth.userEmail || "Google Drive");
      badge.textContent = `✓ ${modeTag}`;
      badge.className = "auth-badge connected";
      $("modalAccountInfo").textContent = state.auth.authMode === "service_account"
        ? `Conta de Serviço ativa (${state.auth.userEmail || "Google Drive"})`
        : `${state.auth.userName || ""} (${state.auth.userEmail || ""})`.trim();
      $("btnModalReconnect").classList.toggle("hidden", state.auth.authMode === "service_account");
      $("btnLogout").classList.toggle("hidden", state.auth.authMode === "service_account");

      setup.classList.add("hidden");
      notReady.classList.add("hidden");
      dash.classList.remove("hidden");
      return;
    }

    badge.textContent = "Google Drive desconectado";
    badge.className = "auth-badge disconnected";
    dash.classList.add("hidden");

    if (!isAdmin()) {
      setup.classList.add("hidden");
      notReady.classList.remove("hidden");
      return;
    }

    notReady.classList.add("hidden");
    setup.classList.remove("hidden");

    if (state.auth && state.auth.hasCredentials && state.auth.authMode !== "service_account") {
      $("stepCredentials").classList.add("hidden");
      $("stepLogin").classList.remove("hidden");
    } else {
      $("stepCredentials").classList.remove("hidden");
      $("stepLogin").classList.add("hidden");
    }
    if (state.auth && state.auth.error) {
      showAlert(state.auth.error, "warning");
    }
  }

  function populateAnoSelect(emp) {
    const sel = $("selAno");
    sel.innerHTML = "";
    const anos = getEmpresaAnos(emp);
    for (const y of anos) {
      const opt = document.createElement("option");
      opt.value = String(y);
      opt.textContent = String(y);
      sel.appendChild(opt);
    }
    if (!anos.includes(state.ano)) state.ano = anos[0];
    sel.value = String(state.ano);
  }

  function populateMesSelect(disponiveis = null) {
    const sel = $("selMes");
    sel.innerHTML = "";
    const list = disponiveis && disponiveis.length > 0
      ? disponiveis.map((m) => ({ num: m.numero, label: m.pasta || `${String(m.numero).padStart(2, "0")} - ${m.nome.toUpperCase()}` }))
      : MESES.map((nome, idx) => ({ num: idx + 1, label: `${String(idx + 1).padStart(2, "0")} - ${nome.toUpperCase()}` }));

    for (const item of list) {
      const opt = document.createElement("option");
      opt.value = item.num;
      opt.textContent = item.label;
      sel.appendChild(opt);
    }
    if (list.some((i) => i.num === state.mes)) {
      sel.value = String(state.mes);
    } else if (list.length > 0) {
      state.mes = list[0].num;
      sel.value = String(state.mes);
    }
  }

  function renderEmpresasSelect() {
    const sel = $("selEmpresa");
    sel.innerHTML = "";

    if (state.empresas.length === 0) {
      $("noEmpresaState").classList.remove("hidden");
      $("resumoContainer").classList.add("hidden");
      $("btnCarregar").disabled = true;
      populateAnoSelect(null);
      return;
    }

    $("noEmpresaState").classList.add("hidden");
    $("btnCarregar").disabled = false;

    for (const emp of state.empresas) {
      const opt = document.createElement("option");
      opt.value = emp.id;
      opt.textContent = `${emp.nome} (${modoLabel(emp.modo)})`;
      sel.appendChild(opt);
    }

    if (!state.selectedEmpresaId || !state.empresas.some((e) => e.id === state.selectedEmpresaId)) {
      state.selectedEmpresaId = state.empresas[0].id;
    }
    sel.value = state.selectedEmpresaId;
    const emp = getSelectedEmpresa();
    populateAnoSelect(emp);
    syncTipoButtonsWithEmpresa();
  }

  function syncTipoButtonsWithEmpresa() {
    const emp = getSelectedEmpresa();
    const btnS = document.querySelector('#tipoSegmented button[data-tipo="S"]');
    const btnA = document.querySelector('#tipoSegmented button[data-tipo="A"]');
    if (!emp) return;

    const usaS = emp.modo === "S" || emp.modo === "SA";
    const usaA = emp.modo === "A" || emp.modo === "SA";

    btnS.disabled = !usaS;
    btnA.disabled = !usaA;

    if (state.tipo === "S" && !usaS) state.tipo = "A";
    if (state.tipo === "A" && !usaA) state.tipo = "S";

    btnS.classList.toggle("active", state.tipo === "S");
    btnA.classList.toggle("active", state.tipo === "A");
    $("mesField").classList.toggle("hidden", state.tipo !== "A");
  }

  async function onEmpresaChanged(autoLoad = false) {
    state.selectedEmpresaId = $("selEmpresa").value;
    const emp = getSelectedEmpresa();
    populateAnoSelect(emp);
    syncTipoButtonsWithEmpresa();
    if (state.tipo === "A") await loadMesesAcertoSilently();
    if (autoLoad) await carregarResumo(false);
  }

  async function onAnoChanged() {
    state.ano = parseInt($("selAno").value, 10) || state.anoAtualSistema;
    if (state.tipo === "A") await loadMesesAcertoSilently();
    await carregarResumo(false);
  }

  async function loadMesesAcertoSilently() {
    const emp = getSelectedEmpresa();
    if (!emp) return populateMesSelect();
    try {
      const meses = await api(`/api/resumo/acerto/meses?empresaId=${encodeURIComponent(emp.id)}&ano=${state.ano}`);
      populateMesSelect(meses);
    } catch (_) {
      populateMesSelect();
    }
  }

  function setLoading(loading, text = "Lendo planilhas do Google Drive...") {
    $("loadingOverlay").classList.toggle("hidden", !loading);
    $("loadingText").textContent = text;
    $("btnCarregar").disabled = loading;
    if (loading) $("resumoContainer").classList.add("hidden");
  }

  async function carregarResumo(forceRefresh = false) {
    const emp = getSelectedEmpresa();
    if (!emp) return;

    showAlert("");
    state.ano = parseInt($("selAno").value, 10) || state.anoAtualSistema;
    state.mes = parseInt($("selMes").value, 10) || 1;
    state.searchQuery = "";
    $("inpBuscaSocio").value = "";

    setLoading(true, forceRefresh
      ? `Atualizando dados de ${state.ano} do Google Drive...`
      : `Carregando resumo (${state.ano})...`);

    try {
      if (state.tipo === "S") {
        const url = `/api/resumo/socios?empresaId=${encodeURIComponent(emp.id)}&ano=${state.ano}&refresh=${forceRefresh}`;
        state.resumoSocios = await api(url);
        state.resumoAcerto = null;
        state.activeTabIndex = 12;
        renderResumoSocios();
      } else {
        const url = `/api/resumo/acerto?empresaId=${encodeURIComponent(emp.id)}&ano=${state.ano}&mes=${state.mes}&refresh=${forceRefresh}`;
        state.resumoAcerto = await api(url);
        state.resumoSocios = null;
        state.activeTabIndex = 4;
        renderResumoAcerto();
      }

      $("resumoContainer").classList.remove("hidden");
      $("btnImprimir").disabled = false;
      $("btnExportar").disabled = false;
    } catch (err) {
      showAlert(err.message, "error");
      $("btnImprimir").disabled = true;
      $("btnExportar").disabled = true;
    } finally {
      setLoading(false);
    }
  }

  function renderStatsAndAvisos(stats, avisos) {
    $("statsText").textContent = isSocio()
      ? `Atualizado às ${stats.atualizadoEm}`
      : `${stats.totalArquivos} planilha(s) lida(s) (${stats.baixados} baixada(s), ${stats.doCache} do cache) em ${(stats.tempoMs / 1000).toFixed(2)}s • Atualizado às ${stats.atualizadoEm}`;

    const avisosBox = $("avisosBox");
    if (avisos && avisos.length > 0) {
      avisosBox.textContent = "Avisos: " + avisos.join(" | ");
      avisosBox.classList.remove("hidden");
    } else {
      avisosBox.classList.add("hidden");
    }
  }

  function sortRows(rows) {
    const q = state.searchQuery.trim().toLowerCase();
    const filtered = q ? rows.filter((r) => r.socio.toLowerCase().includes(q)) : [...rows];
    filtered.sort((a, b) => {
      const va = a[state.sortCol];
      const vb = b[state.sortCol];
      const cmp = typeof va === "string" ? va.localeCompare(vb, "pt-BR") : (va || 0) - (vb || 0);
      return state.sortAsc ? cmp : -cmp;
    });
    return filtered;
  }

  function renderResumoSocios() {
    const data = state.resumoSocios;
    if (!data) return;

    renderStatsAndAvisos(data.stats, data.avisos);

    // Visão otimizada para SÓCIO: mostra os 12 meses de uma vez + Total Anual
    if (isSocio()) {
      const anual = data.abas[12] || data.abas[0];
      const t = anual.total;
      $("tabsBar").innerHTML = "";
      $("printEmpresa").textContent = `${data.empresaNome} — Extrato Individual (${state.user.socioNome})`;
      $("printSubtitulo").textContent = `Resumo Mês a Mês • Ano ${data.ano} • Atualizado em ${data.stats.atualizadoEm}`;

      $("kpiGrid").innerHTML = `
        <div class="kpi-card"><div class="kpi-label">Valor Total (${data.ano})</div><div class="kpi-value">${fmtBRL.format(t.valorTotal)}</div></div>
        <div class="kpi-card"><div class="kpi-label">Recebimento</div><div class="kpi-value accent">${fmtBRL.format(t.recebimento)}</div></div>
        <div class="kpi-card"><div class="kpi-label">Percentual</div><div class="kpi-value">${fmtPct.format(t.percentual)}</div></div>
        <div class="kpi-card"><div class="kpi-label">Mercadorias Pagas</div><div class="kpi-value">${fmtBRL.format(t.mercPagas)}</div></div>
        <div class="kpi-card"><div class="kpi-label">Sócio Ganhou</div><div class="kpi-value ${kpiSignClass(t.valSocio)}">${fmtBRL.format(t.valSocio)}</div></div>
        <div class="kpi-card"><div class="kpi-label">Firma Ganhou</div><div class="kpi-value ${kpiSignClass(t.valFirma)}">${fmtBRL.format(t.valFirma)}</div></div>
      `;

      $("resumoThead").innerHTML = `<tr>
        <th class="col-left">Mês</th>
        <th>Valor Total</th>
        <th>Recebimento</th>
        <th class="col-center">Percentual</th>
        <th>Mercadorias Pagas</th>
        <th>Sócio Ganhou</th>
        <th>Firma Ganhou</th>
      </tr>`;

      const meses = data.abas.slice(0, 12);
      $("resumoTbody").innerHTML = meses.map((m) => {
        const r = m.total;
        return `
          <tr>
            <td class="col-left">${escapeHtml(m.nome)}</td>
            <td>${fmtBRL.format(r.valorTotal)}</td>
            <td>${fmtBRL.format(r.recebimento)}</td>
            <td class="col-center">${fmtPct.format(r.percentual)}</td>
            <td>${fmtBRL.format(r.mercPagas)}</td>
            <td class="${cellSignClass(r.valSocio)}">${fmtBRL.format(r.valSocio)}</td>
            <td class="${cellSignClass(r.valFirma)}">${fmtBRL.format(r.valFirma)}</td>
          </tr>`;
      }).join("");

      $("resumoTfoot").innerHTML = `
        <tr>
          <td class="col-left">TOTAL ${data.ano}</td>
          <td>${fmtBRL.format(t.valorTotal)}</td>
          <td>${fmtBRL.format(t.recebimento)}</td>
          <td class="col-center">${fmtPct.format(t.percentual)}</td>
          <td>${fmtBRL.format(t.mercPagas)}</td>
          <td class="${cellSignClass(t.valSocio)}">${fmtBRL.format(t.valSocio)}</td>
          <td class="${cellSignClass(t.valFirma)}">${fmtBRL.format(t.valFirma)}</td>
        </tr>`;
      return;
    }

    // Visão Admin / Gestor: abas de meses + Resumo Anual com todos os sócios
    const tabsBar = $("tabsBar");
    tabsBar.innerHTML = "";
    data.abas.forEach((aba, idx) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = `tab-btn ${aba.numero === 13 ? "tab-total" : ""} ${idx === state.activeTabIndex ? "active" : ""}`;
      btn.textContent = aba.nome;
      btn.addEventListener("click", () => {
        state.activeTabIndex = idx;
        renderResumoSocios();
      });
      tabsBar.appendChild(btn);
    });

    const aba = data.abas[state.activeTabIndex] || data.abas[0];
    $("printEmpresa").textContent = `${data.empresaNome} — Resultado de Ganhos por Sócio`;
    $("printSubtitulo").textContent = `${aba.nome.toUpperCase()} • Ano ${data.ano} • Gerado em ${data.stats.atualizadoEm}`;

    const t = aba.total;
    $("kpiGrid").innerHTML = `
      <div class="kpi-card"><div class="kpi-label">Valor Total</div><div class="kpi-value">${fmtBRL.format(t.valorTotal)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Recebimento</div><div class="kpi-value accent">${fmtBRL.format(t.recebimento)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Percentual Geral</div><div class="kpi-value">${fmtPct.format(t.percentual)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Mercadorias Pagas</div><div class="kpi-value">${fmtBRL.format(t.mercPagas)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Sócio Ganhou</div><div class="kpi-value ${kpiSignClass(t.valSocio)}">${fmtBRL.format(t.valSocio)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Firma Ganhou</div><div class="kpi-value ${kpiSignClass(t.valFirma)}">${fmtBRL.format(t.valFirma)}</div></div>
    `;

    const cols = [
      { key: "socio", label: "Sócio", cls: "col-left" },
      { key: "valorTotal", label: "Valor Total" },
      { key: "recebimento", label: "Recebimento" },
      { key: "percentual", label: "Percentual", cls: "col-center" },
      { key: "mercPagas", label: "Mercadorias Pagas" },
      { key: "valSocio", label: "Sócio Ganhou" },
      { key: "valFirma", label: "Firma Ganhou" },
    ];

    $("resumoThead").innerHTML = `<tr>${cols.map((c) => {
      const arrow = state.sortCol === c.key ? (state.sortAsc ? " ▲" : " ▼") : "";
      return `<th class="${c.cls || ""}" data-sort="${c.key}">${c.label}${arrow}</th>`;
    }).join("")}</tr>`;
    bindSortHeaders(renderResumoSocios);

    const rows = sortRows(aba.linhas);
    $("resumoTbody").innerHTML = rows.map((r) => `
      <tr>
        <td class="col-left">${escapeHtml(r.socio)}</td>
        <td>${fmtBRL.format(r.valorTotal)}</td>
        <td>${fmtBRL.format(r.recebimento)}</td>
        <td class="col-center">${fmtPct.format(r.percentual)}</td>
        <td>${fmtBRL.format(r.mercPagas)}</td>
        <td class="${cellSignClass(r.valSocio)}">${fmtBRL.format(r.valSocio)}</td>
        <td class="${cellSignClass(r.valFirma)}">${fmtBRL.format(r.valFirma)}</td>
      </tr>
    `).join("");

    $("resumoTfoot").innerHTML = `
      <tr>
        <td class="col-left">TOTAIS (${rows.length})</td>
        <td>${fmtBRL.format(t.valorTotal)}</td>
        <td>${fmtBRL.format(t.recebimento)}</td>
        <td class="col-center">${fmtPct.format(t.percentual)}</td>
        <td>${fmtBRL.format(t.mercPagas)}</td>
        <td class="${cellSignClass(t.valSocio)}">${fmtBRL.format(t.valSocio)}</td>
        <td class="${cellSignClass(t.valFirma)}">${fmtBRL.format(t.valFirma)}</td>
      </tr>
    `;
  }

  function renderResumoAcerto() {
    const data = state.resumoAcerto;
    if (!data) return;

    renderStatsAndAvisos(data.stats, data.avisos);

    const tabsBar = $("tabsBar");
    tabsBar.innerHTML = "";
    data.abas.forEach((aba, idx) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = `tab-btn ${aba.numero === 5 ? "tab-total" : ""} ${idx === state.activeTabIndex ? "active" : ""}`;
      btn.textContent = aba.nome;
      btn.addEventListener("click", () => {
        state.activeTabIndex = idx;
        renderResumoAcerto();
      });
      tabsBar.appendChild(btn);
    });

    const aba = data.abas[state.activeTabIndex] || data.abas[0];
    $("printEmpresa").textContent = `${data.empresaNome} — Resumo de Acerto Semanal`;
    $("printSubtitulo").textContent = `${data.mesNome.toUpperCase()} / ${data.ano} • ${aba.nome} • Gerado em ${data.stats.atualizadoEm}`;

    const t = aba.total;
    $("kpiGrid").innerHTML = `
      <div class="kpi-card"><div class="kpi-label">Cobrança</div><div class="kpi-value">${fmtBRL.format(t.cobranca)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Recebimento (${fmtPct.format(t.percentual)})</div><div class="kpi-value accent">${fmtBRL.format(t.recebimento)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Total Despesas</div><div class="kpi-value negative">${fmtBRL.format(t.totalDespesas)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Total a Pagar</div><div class="kpi-value">${fmtBRL.format(t.totalAPagar)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Total Pagamento</div><div class="kpi-value positive">${fmtBRL.format(t.totalPagamento)}</div></div>
      <div class="kpi-card"><div class="kpi-label">Falta / Sobra</div><div class="kpi-value ${kpiSignClass(t.faltaSobra)}">${fmtBRL.format(t.faltaSobra)}</div></div>
    `;

    const cols = [
      { key: "socio", label: "Nome", cls: "col-left" },
      { key: "cobranca", label: "Cobrança" },
      { key: "qtdNotas", label: "Notas", cls: "col-center" },
      { key: "recebimento", label: "Recebimento" },
      { key: "percentual", label: "%", cls: "col-center" },
      { key: "combustivel", label: "Combustível" },
      { key: "refeicao", label: "Refeição" },
      { key: "hotel", label: "Hotel" },
      { key: "lavaJato", label: "Lava Jato" },
      { key: "oficina", label: "Oficina" },
      { key: "fichas", label: "Fichas" },
      { key: "vale", label: "Vale" },
      { key: "valeAjudante", label: "Vale Ajud." },
      { key: "totalDespesas", label: "Tot. Desp." },
      { key: "totalAPagar", label: "Tot. Pagar" },
      { key: "pix", label: "PIX" },
      { key: "deposito", label: "Depósito" },
      { key: "dinheiro", label: "Dinheiro" },
      { key: "cheque", label: "Cheque" },
      { key: "totalPagamento", label: "Tot. Pgto" },
      { key: "faltaSobra", label: "Falta/Sobra" },
    ];

    $("resumoThead").innerHTML = `
      <tr class="group-row">
        <th colspan="5" class="grp-resumo">Resumo</th>
        <th colspan="9" class="grp-despesas">Despesas</th>
        <th colspan="7" class="grp-recebimentos">Recebimentos</th>
      </tr>
      <tr>${cols.map((c) => {
        const arrow = state.sortCol === c.key ? (state.sortAsc ? " ▲" : " ▼") : "";
        return `<th class="${c.cls || ""}" data-sort="${c.key}">${c.label}${arrow}</th>`;
      }).join("")}</tr>
    `;
    bindSortHeaders(renderResumoAcerto);

    const rows = sortRows(aba.linhas);
    $("resumoTbody").innerHTML = rows.map((r) => `
      <tr>
        <td class="col-left">${escapeHtml(r.socio)}</td>
        <td>${fmtBRL.format(r.cobranca)}</td>
        <td class="col-center">${fmtInt.format(r.qtdNotas)}</td>
        <td>${fmtBRL.format(r.recebimento)}</td>
        <td class="col-center val-bold">${fmtPct.format(r.percentual)}</td>
        <td>${fmtBRL.format(r.combustivel)}</td>
        <td>${fmtBRL.format(r.refeicao)}</td>
        <td>${fmtBRL.format(r.hotel)}</td>
        <td>${fmtBRL.format(r.lavaJato)}</td>
        <td>${fmtBRL.format(r.oficina)}</td>
        <td>${fmtBRL.format(r.fichas)}</td>
        <td>${fmtBRL.format(r.vale)}</td>
        <td>${fmtBRL.format(r.valeAjudante)}</td>
        <td class="val-bold">${fmtBRL.format(r.totalDespesas)}</td>
        <td class="val-bold">${fmtBRL.format(r.totalAPagar)}</td>
        <td>${fmtBRL.format(r.pix)}</td>
        <td>${fmtBRL.format(r.deposito)}</td>
        <td>${fmtBRL.format(r.dinheiro)}</td>
        <td>${fmtBRL.format(r.cheque)}</td>
        <td class="val-bold">${fmtBRL.format(r.totalPagamento)}</td>
        <td class="${cellSignClass(r.faltaSobra)}">${fmtBRL.format(r.faltaSobra)}</td>
      </tr>
    `).join("");

    $("resumoTfoot").innerHTML = `
      <tr>
        <td class="col-left">TOTAIS (${rows.length})</td>
        <td>${fmtBRL.format(t.cobranca)}</td>
        <td class="col-center">${fmtInt.format(t.qtdNotas)}</td>
        <td>${fmtBRL.format(t.recebimento)}</td>
        <td class="col-center">${fmtPct.format(t.percentual)}</td>
        <td>${fmtBRL.format(t.combustivel)}</td>
        <td>${fmtBRL.format(t.refeicao)}</td>
        <td>${fmtBRL.format(t.hotel)}</td>
        <td>${fmtBRL.format(t.lavaJato)}</td>
        <td>${fmtBRL.format(t.oficina)}</td>
        <td>${fmtBRL.format(t.fichas)}</td>
        <td>${fmtBRL.format(t.vale)}</td>
        <td>${fmtBRL.format(t.valeAjudante)}</td>
        <td>${fmtBRL.format(t.totalDespesas)}</td>
        <td>${fmtBRL.format(t.totalAPagar)}</td>
        <td>${fmtBRL.format(t.pix)}</td>
        <td>${fmtBRL.format(t.deposito)}</td>
        <td>${fmtBRL.format(t.dinheiro)}</td>
        <td>${fmtBRL.format(t.cheque)}</td>
        <td>${fmtBRL.format(t.totalPagamento)}</td>
        <td class="${cellSignClass(t.faltaSobra)}">${fmtBRL.format(t.faltaSobra)}</td>
      </tr>
    `;
  }

  function bindSortHeaders(renderFn) {
    document.querySelectorAll("#resumoThead th[data-sort]").forEach((th) => {
      th.addEventListener("click", () => {
        const col = th.getAttribute("data-sort");
        if (state.sortCol === col) state.sortAsc = !state.sortAsc;
        else {
          state.sortCol = col;
          state.sortAsc = col === "socio";
        }
        renderFn();
      });
    });
  }

  // ===== Modal Empresas (Admin) =====
  function openEmpresasModal(selectId = null) {
    renderEmpresasModalList(selectId);
    if (selectId) {
      const emp = state.empresas.find((e) => e.id === selectId);
      if (emp) fillEmpresaForm(emp);
    } else if (state.empresas.length > 0) {
      fillEmpresaForm(state.empresas[0]);
    } else {
      resetEmpresaForm();
    }
    $("modalEmpresas").classList.remove("hidden");
  }

  function renderEmpresasModalList(activeId = "") {
    const container = $("empresasList");
    if (state.empresas.length === 0) {
      container.innerHTML = `<p class="muted small">Nenhuma empresa cadastrada.</p>`;
      return;
    }
    container.innerHTML = state.empresas.map((e) => `
      <div class="empresa-item ${e.id === activeId ? "active" : ""}" data-emp-id="${e.id}">
        <span>${escapeHtml(e.nome)}</span>
        <span class="mode-pill">${modoLabel(e.modo)}</span>
      </div>
    `).join("");

    container.querySelectorAll(".empresa-item").forEach((el) => {
      el.addEventListener("click", () => {
        const emp = state.empresas.find((item) => item.id === el.dataset.empId);
        if (emp) {
          fillEmpresaForm(emp);
          renderEmpresasModalList(emp.id);
        }
      });
    });
  }

  function saveCurrentYearInputsToState() {
    if (!state.formAnoAtivo) return;
    state.formPastasPorAno[state.formAnoAtivo] = {
      sociosId: $("empPastaSociosId").value.trim(),
      sociosNome: $("empPastaSociosNome").textContent.replace("Pasta selecionada: ", "").trim(),
      acertoId: $("empPastaAcertoId").value.trim(),
      acertoNome: $("empPastaAcertoNome").textContent.replace("Pasta selecionada: ", "").trim(),
    };
  }

  function renderFormAnosBar() {
    const bar = $("empAnosBar");
    const years = Object.keys(state.formPastasPorAno).sort((a, b) => Number(b) - Number(a));
    if (years.length === 0) {
      const y = String(state.anoAtualSistema);
      state.formPastasPorAno[y] = { sociosId: "", sociosNome: "", acertoId: "", acertoNome: "" };
      state.formAnoAtivo = y;
      years.push(y);
    }
    if (!state.formPastasPorAno[state.formAnoAtivo]) state.formAnoAtivo = years[0];

    bar.innerHTML = years.map((y) => `
      <button type="button" class="year-pill ${y === state.formAnoAtivo ? "active" : ""}" data-form-year="${y}">${y}</button>
    `).join("");

    bar.querySelectorAll("[data-form-year]").forEach((btn) => {
      btn.addEventListener("click", () => {
        saveCurrentYearInputsToState();
        state.formAnoAtivo = btn.dataset.formYear;
        renderFormAnosBar();
        loadActiveYearInputsFromState();
      });
    });

    $("btnRemoveAno").classList.toggle("hidden", years.length <= 1);
  }

  function loadActiveYearInputsFromState() {
    const y = state.formAnoAtivo;
    const ref = state.formPastasPorAno[y] || { sociosId: "", sociosNome: "", acertoId: "", acertoNome: "" };
    $("lblPastaSocios").textContent = `Pasta de Sócios no Google Drive (${y})`;
    $("lblPastaAcerto").textContent = `Pasta Raiz de Acerto Semanal no Google Drive (${y})`;
    $("empPastaSociosId").value = ref.sociosId || "";
    $("empPastaSociosNome").textContent = ref.sociosNome ? `Pasta selecionada: ${ref.sociosNome}` : "";
    $("empPastaAcertoId").value = ref.acertoId || "";
    $("empPastaAcertoNome").textContent = ref.acertoNome ? `Pasta selecionada: ${ref.acertoNome}` : "";
  }

  function resetEmpresaForm() {
    $("formEmpresaTitulo").textContent = "Nova Empresa";
    $("empId").value = "";
    $("empNome").value = "";
    setFormModo("S");
    const y = String(state.anoAtualSistema);
    state.formPastasPorAno = { [y]: { sociosId: "", sociosNome: "", acertoId: "", acertoNome: "" } };
    state.formAnoAtivo = y;
    renderFormAnosBar();
    loadActiveYearInputsFromState();
    $("celValorTotal").value = "B12";
    $("celRecebimento").value = "D11";
    $("celMercPagas").value = "F6";
    $("celValSocio").value = "E20";
    $("celValFirma").value = "E19";
    $("btnDeleteEmpresa").classList.add("hidden");
    renderEmpresasModalList("");
  }

  function fillEmpresaForm(emp) {
    $("formEmpresaTitulo").textContent = `Editar: ${emp.nome}`;
    $("empId").value = emp.id;
    $("empNome").value = emp.nome;
    setFormModo(emp.modo || "S");

    state.formPastasPorAno = {};
    if (emp.pastasPorAno && Object.keys(emp.pastasPorAno).length > 0) {
      for (const [y, ref] of Object.entries(emp.pastasPorAno)) {
        state.formPastasPorAno[y] = {
          sociosId: ref.sociosId || "",
          sociosNome: ref.sociosNome || "",
          acertoId: ref.acertoId || "",
          acertoNome: ref.acertoNome || "",
        };
      }
    } else {
      const y = String(state.anoAtualSistema);
      state.formPastasPorAno[y] = {
        sociosId: emp.pastaSociosId || "",
        sociosNome: emp.pastaSociosNome || "",
        acertoId: emp.pastaAcertoId || "",
        acertoNome: emp.pastaAcertoNome || "",
      };
    }

    const years = Object.keys(state.formPastasPorAno).sort((a, b) => Number(b) - Number(a));
    state.formAnoAtivo = state.formPastasPorAno[String(state.ano)] ? String(state.ano) : years[0];
    renderFormAnosBar();
    loadActiveYearInputsFromState();

    const c = emp.celulas || {};
    $("celValorTotal").value = c.valorTotal || "B12";
    $("celRecebimento").value = c.recebimento || "D11";
    $("celMercPagas").value = c.mercPagas || "F6";
    $("celValSocio").value = c.valSocio || "E20";
    $("celValFirma").value = c.valFirma || "E19";
    $("btnDeleteEmpresa").classList.remove("hidden");
  }

  function setFormModo(modo) {
    document.querySelectorAll('input[name="empModo"]').forEach((r) => { r.checked = r.value === modo; });
    const usaS = modo === "S" || modo === "SA";
    const usaA = modo === "A" || modo === "SA";
    $("groupPastaSocios").classList.toggle("hidden", !usaS);
    $("groupCelulasSocios").classList.toggle("hidden", !usaS);
    $("groupPastaAcerto").classList.toggle("hidden", !usaA);
  }

  function getFormModo() {
    const checked = document.querySelector('input[name="empModo"]:checked');
    return checked ? checked.value : "S";
  }

  // ===== Modal Usuários / Acessos dos Sócios =====
  async function openUsersModal() {
    $("modalUsersTitle").textContent = isGestor() ? "Acessos dos Sócios" : "Usuários e Permissões";
    $("usrRoleField").classList.toggle("hidden", isGestor());
    $("modalUsers").classList.remove("hidden");
    await reloadUsersList();
    resetUserForm();
  }

  async function reloadUsersList(activeId = "") {
    state.users = await api("/api/users");
    const container = $("usersList");
    if (state.users.length === 0) {
      container.innerHTML = `<p class="muted small">Nenhum acesso cadastrado ainda.</p>`;
      return;
    }
    container.innerHTML = state.users.map((u) => `
      <div class="empresa-item ${u.id === activeId ? "active" : ""}" data-usr-id="${u.id}">
        <div>
          <strong>${escapeHtml(u.username)}</strong>
          <div class="muted small">${escapeHtml(u.socioNome || u.nome)}${u.ativo ? "" : " (Bloqueado)"}</div>
        </div>
        <span class="mode-pill">${roleLabel(u.role)}</span>
      </div>
    `).join("");

    container.querySelectorAll(".empresa-item").forEach((el) => {
      el.addEventListener("click", () => {
        const u = state.users.find((item) => item.id === el.dataset.usrId);
        if (u) {
          fillUserForm(u);
          reloadUsersList(u.id);
        }
      });
    });
  }

  function renderUserEmpresasCheckboxes(selectedIds = []) {
    const wrap = $("usrEmpresasCheckboxes");
    if (state.empresas.length === 0) {
      wrap.innerHTML = `<span class="muted small">Nenhuma empresa disponível.</span>`;
      return;
    }
    wrap.innerHTML = state.empresas.map((e) => `
      <label class="checkbox-chip">
        <input type="checkbox" name="usrEmpId" value="${e.id}" ${selectedIds.includes(e.id) ? "checked" : ""} />
        <span>${escapeHtml(e.nome)}</span>
      </label>
    `).join("");
  }

  function syncUserRoleFields() {
    const role = isGestor() ? "socio" : $("usrRole").value;
    $("usrEmpresasField").classList.toggle("hidden", role === "admin");
    $("usrSocioNomeField").classList.toggle("hidden", role !== "socio");
  }

  function resetUserForm() {
    $("formUserTitle").textContent = isGestor() ? "Novo Acesso de Sócio" : "Novo Usuário";
    $("usrId").value = "";
    $("usrUsername").value = "";
    $("usrNome").value = "";
    $("usrPassword").value = "";
    $("usrPassHint").textContent = "(mínimo 4 caracteres)";
    $("usrRole").value = isGestor() ? "socio" : "gestor";
    $("usrSocioNome").value = "";
    $("usrAtivo").checked = true;
    renderUserEmpresasCheckboxes(state.selectedEmpresaId ? [state.selectedEmpresaId] : []);
    syncUserRoleFields();
    $("btnDeleteUser").classList.add("hidden");
    renderUserShortcutBox(null);
  }

  function fillUserForm(u) {
    $("formUserTitle").textContent = `Editar: ${u.username}`;
    $("usrId").value = u.id;
    $("usrUsername").value = u.username;
    $("usrNome").value = u.nome || "";
    $("usrPassword").value = "";
    $("usrPassHint").textContent = "(deixe vazio para manter a atual)";
    $("usrRole").value = u.role;
    $("usrSocioNome").value = u.socioNome || "";
    $("usrAtivo").checked = u.ativo !== false;
    renderUserEmpresasCheckboxes(u.empresaIds || []);
    syncUserRoleFields();
    $("btnDeleteUser").classList.toggle("hidden", u.id === state.user?.id);
    renderUserShortcutBox(u);
  }

  function renderUserShortcutBox(u) {
    const box = $("usrShortcutBox");
    if (!u || !u.id) {
      box.classList.add("hidden");
      return;
    }
    box.classList.remove("hidden");

    const hasToken = !!u.accessToken;
    $("btnGenToken").textContent = hasToken ? "↻ Renovar Token" : "Gerar Token";
    $("btnRevokeToken").classList.toggle("hidden", !hasToken);
    $("usrShortcutEmpty").classList.toggle("hidden", hasToken);
    $("usrShortcutReady").classList.toggle("hidden", !hasToken);

    if (!hasToken) return;

    const allowedEmpresas = u.role === "admin"
      ? state.empresas
      : state.empresas.filter((e) => (u.empresaIds || []).includes(e.id));

    const scEmp = $("scEmpresa");
    scEmp.innerHTML = "";
    if (allowedEmpresas.length === 0) {
      const opt = document.createElement("option");
      opt.value = "";
      opt.textContent = "(Painel geral)";
      scEmp.appendChild(opt);
    } else {
      for (const emp of allowedEmpresas) {
        const opt = document.createElement("option");
        opt.value = emp.id;
        opt.textContent = emp.nome;
        scEmp.appendChild(opt);
      }
    }
    syncShortcutAnoTipo(u);
  }

  function syncShortcutAnoTipo(u) {
    const empId = $("scEmpresa").value;
    const emp = state.empresas.find((e) => e.id === empId) || null;

    const scAno = $("scAno");
    scAno.innerHTML = "";
    const anos = getEmpresaAnos(emp);
    for (const y of anos) {
      const opt = document.createElement("option");
      opt.value = String(y);
      opt.textContent = String(y);
      scAno.appendChild(opt);
    }

    const scTipo = $("scTipo");
    const optS = scTipo.querySelector('option[value="S"]');
    const optA = scTipo.querySelector('option[value="A"]');
    if (emp) {
      const usaS = emp.modo === "S" || emp.modo === "SA";
      const usaA = emp.modo === "A" || emp.modo === "SA";
      optS.disabled = !usaS;
      optA.disabled = !usaA;
      if (scTipo.value === "S" && !usaS) scTipo.value = "A";
      if (scTipo.value === "A" && !usaA) scTipo.value = "S";
    } else {
      optS.disabled = false;
      optA.disabled = false;
    }

    updateShortcutUrlPreview(u);
  }

  function updateShortcutUrlPreview(u) {
    const target = u || state.users.find((x) => x.id === $("usrId").value);
    if (!target || !target.accessToken) return;

    const qs = new URLSearchParams();
    qs.set("token", target.accessToken);
    if ($("scEmpresa").value) qs.set("empresaId", $("scEmpresa").value);
    if ($("scAno").value) qs.set("ano", $("scAno").value);
    if ($("scTipo").value) qs.set("tipo", $("scTipo").value);

    $("usrShortcutUrl").value = `${window.location.origin}/acesso?${qs.toString()}`;
  }

  async function copyTextSafe(text) {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const input = $("usrShortcutUrl");
    input.select();
    input.setSelectionRange(0, text.length);
    document.execCommand("copy");
  }

  // ===== Explorador de Pastas do Google Drive =====
  async function openDrivePicker(target) {
    if (!state.auth || !state.auth.authenticated) {
      showAlert("Conecte o Google Drive primeiro.", "warning");
      return;
    }
    state.pickerTarget = target;
    const isSA = state.auth.authMode === "service_account";
    state.pickerShared = isSA;
    state.pickerStack = [];
    $("drivePickerSource").classList.toggle("hidden", isSA);
    $("btnDriveMyDrive").classList.toggle("active", !isSA);
    $("btnDriveShared").classList.toggle("active", isSA);
    $("modalDrivePicker").classList.remove("hidden");
    await loadDriveFolders();
  }

  async function loadDriveFolders() {
    const current = state.pickerStack[state.pickerStack.length - 1] || null;
    $("btnSelectCurrentFolder").disabled = !current;
    renderDriveBreadcrumb();

    const listEl = $("driveFoldersList");
    listEl.innerHTML = `<div class="loading-box"><div class="spinner"></div><p>Carregando pastas...</p></div>`;

    try {
      const qs = new URLSearchParams();
      if (current) qs.set("parentId", current.id);
      if (state.pickerShared) qs.set("shared", "true");
      const folders = await api(`/api/drive/folders?${qs.toString()}`);

      if (folders.length === 0) {
        listEl.innerHTML = `<p class="muted small" style="padding:16px">Nenhuma pasta encontrada aqui. (Se estiver usando Conta de Serviço, verifique se a pasta foi compartilhada com o e-mail dela.)</p>`;
        return;
      }

      listEl.innerHTML = folders.map((f) => `
        <div class="drive-folder-row">
          <div class="drive-folder-name" data-open-id="${f.id}" data-open-name="${escapeHtml(f.name)}">
            <span>📁</span><span>${escapeHtml(f.name)}</span>
          </div>
          <button type="button" class="btn btn-sm btn-secondary" data-pick-id="${f.id}" data-pick-name="${escapeHtml(f.name)}">Selecionar</button>
        </div>
      `).join("");

      listEl.querySelectorAll("[data-open-id]").forEach((el) => {
        el.addEventListener("click", () => {
          state.pickerStack.push({ id: el.dataset.openId, name: el.dataset.openName });
          loadDriveFolders();
        });
      });

      listEl.querySelectorAll("[data-pick-id]").forEach((btn) => {
        btn.addEventListener("click", () => applyPickedFolder(btn.dataset.pickId, btn.dataset.pickName));
      });
    } catch (err) {
      listEl.innerHTML = `<p class="alert alert-error" style="margin:12px">${escapeHtml(err.message)}</p>`;
    }
  }

  function renderDriveBreadcrumb() {
    const bc = $("driveBreadcrumb");
    const rootName = state.pickerShared ? "Pastas Compartilhadas" : "Meu Drive";
    const items = [{ id: "", name: rootName }, ...state.pickerStack];
    bc.innerHTML = items.map((item, idx) =>
      `<button type="button" data-bc-idx="${idx}">${escapeHtml(item.name)}</button>${idx < items.length - 1 ? " / " : ""}`
    ).join("");

    bc.querySelectorAll("button").forEach((btn) => {
      btn.addEventListener("click", () => {
        const idx = parseInt(btn.dataset.bcIdx, 10);
        state.pickerStack = state.pickerStack.slice(0, idx);
        loadDriveFolders();
      });
    });
  }

  function applyPickedFolder(id, name) {
    if (state.pickerTarget === "S") {
      $("empPastaSociosId").value = id;
      $("empPastaSociosNome").textContent = `Pasta selecionada: ${name}`;
    } else {
      $("empPastaAcertoId").value = id;
      $("empPastaAcertoNome").textContent = `Pasta selecionada: ${name}`;
    }
    saveCurrentYearInputsToState();
    $("modalDrivePicker").classList.add("hidden");
  }

  function escapeHtml(str) {
    return String(str || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // ===== Eventos =====
  function bindEvents() {
    // Login do sistema
    $("formLogin").addEventListener("submit", async (e) => {
      e.preventDefault();
      $("loginError").classList.add("hidden");
      $("btnLoginSubmit").disabled = true;
      try {
        await api("/api/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            username: $("loginUser").value.trim(),
            password: $("loginPass").value,
          }),
        });
        $("loginPass").value = "";
        await bootstrap();
      } catch (err) {
        $("loginError").textContent = err.message;
        $("loginError").classList.remove("hidden");
      } finally {
        $("btnLoginSubmit").disabled = false;
      }
    });

    $("btnAppLogout").addEventListener("click", async () => {
      await api("/api/logout", { method: "POST" });
      state.loggedIn = false;
      state.user = null;
      state.resumoSocios = null;
      state.resumoAcerto = null;
      renderShellVisibility();
    });

    // Credenciais Google Drive
    $("btnUploadCredentials").addEventListener("click", async () => {
      const file = $("fileCredentials").files[0];
      if (!file) return showAlert("Selecione o arquivo JSON primeiro.", "warning");
      await saveCredentialsText(await file.text());
    });

    $("btnPasteCredentials").addEventListener("click", async () => {
      const text = $("txtCredentials").value.trim();
      if (!text) return showAlert("Cole o conteúdo JSON primeiro.", "warning");
      await saveCredentialsText(text);
    });

    $("btnReplaceCreds").addEventListener("click", () => {
      $("stepLogin").classList.add("hidden");
      $("stepCredentials").classList.remove("hidden");
    });

    $("btnTrocarJsonModal").addEventListener("click", () => {
      $("modalEmpresas").classList.add("hidden");
      $("dashboardSection").classList.add("hidden");
      $("setupSection").classList.remove("hidden");
      $("stepLogin").classList.add("hidden");
      $("stepCredentials").classList.remove("hidden");
    });

    // Filtros
    $("selEmpresa").addEventListener("change", () => onEmpresaChanged(true));
    $("selAno").addEventListener("change", () => onAnoChanged());
    $("selMes").addEventListener("change", () => carregarResumo(false));

    document.querySelectorAll("#tipoSegmented .seg-btn").forEach((btn) => {
      btn.addEventListener("click", async () => {
        if (btn.disabled) return;
        state.tipo = btn.dataset.tipo;
        syncTipoButtonsWithEmpresa();
        if (state.tipo === "A") await loadMesesAcertoSilently();
        await carregarResumo(false);
      });
    });

    $("btnCarregar").addEventListener("click", () => carregarResumo(true));
    $("btnImprimir").addEventListener("click", () => window.print());
    $("btnExportar").addEventListener("click", () => {
      const emp = getSelectedEmpresa();
      if (!emp) return;
      if (state.tipo === "S") {
        window.location.href = `/api/export/socios?empresaId=${encodeURIComponent(emp.id)}&ano=${state.ano}`;
      } else {
        window.location.href = `/api/export/acerto?empresaId=${encodeURIComponent(emp.id)}&ano=${state.ano}&mes=${state.mes}`;
      }
    });

    $("inpBuscaSocio").addEventListener("input", (e) => {
      state.searchQuery = e.target.value;
      if (state.tipo === "S") renderResumoSocios();
      else renderResumoAcerto();
    });

    $("btnConfig").addEventListener("click", () => openEmpresasModal(state.selectedEmpresaId));
    $("btnUsers").addEventListener("click", () => openUsersModal());
    $("btnNovaEmpresaEmpty").addEventListener("click", () => {
      openEmpresasModal();
      resetEmpresaForm();
    });

    $("btnClearCache").addEventListener("click", async () => {
      try {
        const res = await api("/api/cache/clear", { method: "POST" });
        showAlert(`Cache limpo (${res.removed} arquivo(s) removido(s)).`, "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("btnShutdown").addEventListener("click", async () => {
      if (!confirm("Deseja encerrar o aplicativo Resumo de Planilhas?")) return;
      try { await api("/api/shutdown", { method: "POST" }); } catch (_) {}
      document.body.innerHTML = `<div style="padding:48px;text-align:center;font-family:sans-serif"><h2>Aplicativo encerrado</h2><p>Você já pode fechar esta aba do navegador.</p></div>`;
    });

    document.querySelectorAll("[data-close]").forEach((btn) => {
      btn.addEventListener("click", () => $(btn.dataset.close).classList.add("hidden"));
    });

    // Form Empresa
    $("btnAddEmpresa").addEventListener("click", resetEmpresaForm);
    document.querySelectorAll('input[name="empModo"]').forEach((r) => {
      r.addEventListener("change", () => setFormModo(getFormModo()));
    });

    $("btnAddAno").addEventListener("click", () => {
      saveCurrentYearInputsToState();
      const existing = Object.keys(state.formPastasPorAno).map(Number);
      const suggested = existing.length > 0 ? Math.max(...existing) + 1 : state.anoAtualSistema;
      const input = prompt("Informe o ano para vincular as pastas (ex: 2027):", String(suggested));
      if (!input) return;
      const yearNum = parseInt(input.trim(), 10);
      if (isNaN(yearNum) || yearNum < 2000 || yearNum > 2100) {
        showAlert("Ano inválido. Informe um ano com 4 dígitos (ex: 2027).", "warning");
        return;
      }
      const yKey = String(yearNum);
      if (!state.formPastasPorAno[yKey]) {
        state.formPastasPorAno[yKey] = { sociosId: "", sociosNome: "", acertoId: "", acertoNome: "" };
      }
      state.formAnoAtivo = yKey;
      renderFormAnosBar();
      loadActiveYearInputsFromState();
    });

    $("btnRemoveAno").addEventListener("click", () => {
      const years = Object.keys(state.formPastasPorAno);
      if (years.length <= 1) return;
      if (!confirm(`Remover o vínculo de pastas do ano ${state.formAnoAtivo}?`)) return;
      delete state.formPastasPorAno[state.formAnoAtivo];
      const remaining = Object.keys(state.formPastasPorAno).sort((a, b) => Number(b) - Number(a));
      state.formAnoAtivo = remaining[0];
      renderFormAnosBar();
      loadActiveYearInputsFromState();
    });

    $("btnPickSocios").addEventListener("click", () => openDrivePicker("S"));
    $("btnPickAcerto").addEventListener("click", () => openDrivePicker("A"));

    $("formEmpresa").addEventListener("submit", async (e) => {
      e.preventDefault();
      saveCurrentYearInputsToState();
      const activeRef = state.formPastasPorAno[state.formAnoAtivo] || {};
      const payload = {
        id: $("empId").value,
        nome: $("empNome").value,
        modo: getFormModo(),
        pastaSociosId: activeRef.sociosId || "",
        pastaSociosNome: activeRef.sociosNome || "",
        pastaAcertoId: activeRef.acertoId || "",
        pastaAcertoNome: activeRef.acertoNome || "",
        pastasPorAno: state.formPastasPorAno,
        celulas: {
          valorTotal: $("celValorTotal").value,
          recebimento: $("celRecebimento").value,
          mercPagas: $("celMercPagas").value,
          valSocio: $("celValSocio").value,
          valFirma: $("celValFirma").value,
        },
      };

      try {
        const saved = await api("/api/empresas", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });
        state.empresas = await api("/api/empresas");
        state.selectedEmpresaId = saved.id;
        state.ano = parseInt(state.formAnoAtivo, 10) || state.ano;
        renderEmpresasSelect();
        renderEmpresasModalList(saved.id);
        fillEmpresaForm(saved);
        showAlert(`Empresa "${saved.nome}" salva com sucesso!`, "success");
        $("modalEmpresas").classList.add("hidden");
        await onEmpresaChanged(true);
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("btnDeleteEmpresa").addEventListener("click", async () => {
      const id = $("empId").value;
      if (!id || !confirm("Excluir esta empresa da configuração?")) return;
      try {
        await api(`/api/empresas/${encodeURIComponent(id)}`, { method: "DELETE" });
        state.empresas = await api("/api/empresas");
        state.selectedEmpresaId = state.empresas[0]?.id || "";
        renderEmpresasSelect();
        resetEmpresaForm();
        showAlert("Empresa excluída.", "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("btnLogout").addEventListener("click", async () => {
      if (!confirm("Desconectar esta conta do Google Drive?")) return;
      await api("/api/auth/logout", { method: "POST" });
      $("modalEmpresas").classList.add("hidden");
      await bootstrap();
    });

    // Form Usuários
    $("btnAddUser").addEventListener("click", resetUserForm);
    $("usrRole").addEventListener("change", syncUserRoleFields);

    $("btnBuscarNomesSocios").addEventListener("click", async () => {
      const checked = Array.from(document.querySelectorAll('input[name="usrEmpId"]:checked')).map((el) => el.value);
      const targetEmpId = checked[0] || state.selectedEmpresaId;
      if (!targetEmpId) return showAlert("Marque ao menos uma empresa primeiro.", "warning");

      $("btnBuscarNomesSocios").disabled = true;
      $("btnBuscarNomesSocios").textContent = "Buscando...";
      try {
        const nomes = await api(`/api/socios-nomes?empresaId=${encodeURIComponent(targetEmpId)}&ano=${state.ano}`);
        const dl = $("sociosDatalist");
        dl.innerHTML = nomes.map((n) => `<option value="${escapeHtml(n)}"></option>`).join("");
        $("usrSocioHint").textContent = nomes.length > 0
          ? `Encontrados: ${nomes.join(", ")}`
          : "Nenhum nome encontrado nesta pasta/ano.";
      } catch (err) {
        showAlert(err.message, "error");
      } finally {
        $("btnBuscarNomesSocios").disabled = false;
        $("btnBuscarNomesSocios").textContent = "Buscar do Drive";
      }
    });

    $("formUser").addEventListener("submit", async (e) => {
      e.preventDefault();
      const empresaIds = Array.from(document.querySelectorAll('input[name="usrEmpId"]:checked')).map((el) => el.value);
      const payload = {
        id: $("usrId").value,
        username: $("usrUsername").value.trim(),
        nome: $("usrNome").value.trim(),
        password: $("usrPassword").value,
        role: isGestor() ? "socio" : $("usrRole").value,
        empresaIds,
        socioNome: $("usrSocioNome").value.trim(),
        ativo: $("usrAtivo").checked,
      };
      try {
        const saved = await api("/api/users", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });
        await reloadUsersList(saved.id);
        fillUserForm(saved);
        showAlert(`Acesso "${saved.username}" salvo com sucesso!`, "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("btnDeleteUser").addEventListener("click", async () => {
      const id = $("usrId").value;
      if (!id || !confirm("Excluir este acesso?")) return;
      try {
        await api(`/api/users/${encodeURIComponent(id)}`, { method: "DELETE" });
        await reloadUsersList();
        resetUserForm();
        showAlert("Acesso excluído.", "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    // Token / Atalho de Acesso Direto
    $("btnGenToken").addEventListener("click", async () => {
      const id = $("usrId").value;
      if (!id) return;
      const existing = state.users.find((x) => x.id === id);
      if (existing && existing.accessToken && !confirm("Gerar um novo token vai invalidar os atalhos antigos deste usuário. Continuar?")) {
        return;
      }
      try {
        const updated = await api(`/api/users/${encodeURIComponent(id)}/token`, { method: "POST" });
        await reloadUsersList(updated.id);
        fillUserForm(updated);
        showAlert("Token de acesso direto gerado!", "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("btnRevokeToken").addEventListener("click", async () => {
      const id = $("usrId").value;
      if (!id || !confirm("Revogar o token? Todos os atalhos deste usuário deixarão de funcionar.")) return;
      try {
        const updated = await api(`/api/users/${encodeURIComponent(id)}/token`, { method: "DELETE" });
        await reloadUsersList(updated.id);
        fillUserForm(updated);
        showAlert("Token revogado com sucesso.", "success");
      } catch (err) {
        showAlert(err.message, "error");
      }
    });

    $("scEmpresa").addEventListener("change", () => syncShortcutAnoTipo());
    $("scAno").addEventListener("change", () => updateShortcutUrlPreview());
    $("scTipo").addEventListener("change", () => updateShortcutUrlPreview());

    $("btnCopyShortcut").addEventListener("click", async () => {
      const url = $("usrShortcutUrl").value;
      if (!url) return;
      try {
        await copyTextSafe(url);
        showAlert("Link de acesso copiado para a área de transferência!", "success");
      } catch (err) {
        showAlert("Não foi possível copiar automaticamente: " + err.message, "warning");
      }
    });

    $("btnDownloadShortcut").addEventListener("click", () => {
      const id = $("usrId").value;
      if (!id) return;
      const qs = new URLSearchParams();
      if ($("scEmpresa").value) qs.set("empresaId", $("scEmpresa").value);
      if ($("scAno").value) qs.set("ano", $("scAno").value);
      if ($("scTipo").value) qs.set("tipo", $("scTipo").value);
      window.location.href = `/api/users/${encodeURIComponent(id)}/atalho?${qs.toString()}`;
    });

    // Picker Drive
    $("btnDriveMyDrive").addEventListener("click", () => {
      state.pickerShared = false;
      state.pickerStack = [];
      $("btnDriveMyDrive").classList.add("active");
      $("btnDriveShared").classList.remove("active");
      loadDriveFolders();
    });

    $("btnDriveShared").addEventListener("click", () => {
      state.pickerShared = true;
      state.pickerStack = [];
      $("btnDriveShared").classList.add("active");
      $("btnDriveMyDrive").classList.remove("active");
      loadDriveFolders();
    });

    $("btnSelectCurrentFolder").addEventListener("click", () => {
      const current = state.pickerStack[state.pickerStack.length - 1];
      if (current) applyPickedFolder(current.id, current.name);
    });
  }

  async function saveCredentialsText(text) {
    try {
      const res = await api("/api/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: text,
      });
      showAlert(res.message, "success");
      await bootstrap();
    } catch (err) {
      showAlert(err.message, "error");
    }
  }

  bindEvents();
  bootstrap().catch((err) => showAlert(err.message, "error"));
})();
