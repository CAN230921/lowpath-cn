"use strict";

const side = 8;
const start = 0;
const port = side * side - 1;
const allBeacons = 0b111;
const landmarks = new Map([[1, ["长", "长城"]], [2, ["河", "黄河"]], [4, ["桂", "桂林"]]]);
const trailEvents = new Map([
  ["bell", { text: "檐下铜铃轻响，山径转过一处旧驿。", color: "#ffd690" }],
  ["ferry", { text: "渡舟划开水面，对岸升起一缕炊烟。", color: "#91dcd0" }],
  ["rain", { text: "一阵山雨掠过，远处青峰从雾里浮现。", color: "#c6d6f4" }],
]);
const directions = {
  ArrowUp: [0, -1], ArrowDown: [0, 1], ArrowLeft: [-1, 0], ArrowRight: [1, 0],
};

function step(cell, [dx, dy]) {
  const x = (cell % side) + dx;
  const y = Math.floor(cell / side) + dy;
  return x < 0 || x >= side || y < 0 || y >= side ? -1 : y * side + x;
}

function neighbors(cell) {
  return Object.values(directions).map(direction => step(cell, direction)).filter(next => next >= 0);
}

function shortestRoute(reefs, beacons) {
  const queue = [{ cell: start, mask: 0, length: 0 }];
  const seen = new Set([start]);
  for (let cursor = 0; cursor < queue.length; cursor++) {
    const { cell, mask, length } = queue[cursor];
    if (cell === port && mask === allBeacons) return length;
    for (const next of neighbors(cell)) {
      if (reefs.has(next)) continue;
      const collected = mask | (beacons.get(next) ?? 0);
      if (next === port && collected !== allBeacons) continue;
      const state = collected * (side * side) + next;
      if (seen.has(state)) continue;
      seen.add(state);
      queue.push({ cell: next, mask: collected, length: length + 1 });
    }
  }
  return Infinity;
}

function makeBoard(seed) {
  let randomState = Math.imul(seed, 0x9e3779b1) >>> 0 || 1;
  function randomIndex(size) {
    randomState ^= randomState << 13;
    randomState ^= randomState >>> 17;
    randomState ^= randomState << 5;
    return (randomState >>> 0) % size;
  }

  for (let attempt = 0; attempt < 40; attempt++) {
    const cells = Array.from({ length: port - 1 }, (_, i) => i + 1);
    for (let i = cells.length - 1; i > 0; i--) {
      const j = randomIndex(i + 1);
      [cells[i], cells[j]] = [cells[j], cells[i]];
    }
    const reefs = new Set(cells.slice(3, 19));
    const beacons = new Map(cells.slice(0, 3).map((cell, i) => [cell, 1 << i]));
    const par = shortestRoute(reefs, beacons);
    if (par >= 19 && par <= 34) return { reefs, beacons, par };
  }
  const reefs = new Set([2, 3, 10, 13, 14, 17, 21, 27, 29, 33, 37, 41, 43, 49, 51, 53]);
  const beacons = new Map([[6, 1], [40, 2], [59, 4]]);
  return { reefs, beacons, par: shortestRoute(reefs, beacons) };
}

async function daily() {
  try {
    const response = await fetch("/api/game/daily", { signal: AbortSignal.timeout(5000) });
    if (!response.ok) throw new Error("Daily puzzle unavailable");
    const { date, seed } = await response.json();
    if (/^\d{4}-\d{2}-\d{2}$/.test(date) && seed === Number(date.replaceAll("-", ""))) {
      return { date, seed };
    }
  } catch {
    // Use the same date in Hong Kong when the daily puzzle API is unreachable.
  }
  const parts = new Intl.DateTimeFormat("en", {
    timeZone: "Asia/Hong_Kong", year: "numeric", month: "2-digit", day: "2-digit",
  }).formatToParts(new Date());
  const fields = Object.fromEntries(parts.map(({ type, value }) => [type, value]));
  const date = [fields.year, fields.month, fields.day].join("-");
  return { date, seed: Number(date.replaceAll("-", "")) };
}

async function play() {
  const board = document.getElementById("board");
  const status = document.getElementById("status");
  const undo = document.getElementById("undo");
  const reset = document.getElementById("reset");
  const steps = document.getElementById("steps");
  const beaconCount = document.getElementById("beacons");
  const bestScore = document.getElementById("best");
  const { date, seed } = await daily();
  const { reefs, beacons, par } = makeBoard(seed);
  const bestKey = "shanhe-best-" + date;
  let best = 0;
  try {
    best = Number(localStorage.getItem(bestKey)) || 0;
  } catch {
    // The current game remains playable with storage disabled.
  }
  let path = [{ cell: start, mask: 0 }];
  let message = "";
  let eventRequest;
  let eventAnimation;
  const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");

  document.getElementById("date").textContent = "每日挑战 · " + date;
  const tiles = Array.from({ length: side * side }, (_, cell) => {
    const tile = document.createElement("button");
    tile.type = "button";
    tile.className = "cell";
    tile.addEventListener("click", () => move(cell));
    board.append(tile);
    return tile;
  });

  function render() {
    const { cell, mask } = path.at(-1);
    const won = cell === port;
    const adjacent = neighbors(cell);
    const trail = new Set(path.map(entry => entry.cell));
    tiles.forEach((tile, index) => {
      const beacon = beacons.get(index);
      const reef = reefs.has(index);
      const collected = Boolean(mask & beacon);
      const reachable = !won && !reef && adjacent.includes(index);
      const [symbol, name] = landmarks.get(beacon) ?? ["", ""];
      const kind = reef ? "山岭" : index === port ? "驿站" : beacon ? name + (collected ? "，已走访" : "") : "山径";
      tile.classList.toggle("reef", reef);
      tile.classList.toggle("trail", trail.has(index));
      tile.classList.toggle("boat", index === cell);
      tile.classList.toggle("port", index === port);
      tile.classList.toggle("beacon", Boolean(beacon));
      tile.classList.toggle("collected", collected);
      tile.classList.toggle("reachable", reachable);
      tile.textContent = index === cell ? "▲" : index === port ? "驿" : collected ? "✓" : symbol;
      tile.tabIndex = reachable ? 0 : -1;
      tile.setAttribute("aria-label", "第 " + (Math.floor(index / side) + 1) + " 行第 " + (index % side + 1) + " 列，" + (index === cell ? "当前位置，" : "") + kind);
      tile.setAttribute("aria-disabled", String(!reachable));
    });
    steps.textContent = path.length - 1;
    beaconCount.textContent = [...beacons.values()].filter(bit => mask & bit).length + "/3";
    bestScore.textContent = best || "—";
    undo.disabled = reset.disabled = path.length === 1;
    status.textContent = message;
  }

  function clearEvent() {
    eventRequest?.abort();
    eventAnimation?.cancel();
  }

  async function showEvent(from, move, firstVisit) {
    const controller = new AbortController();
    eventRequest = controller;
    try {
      const response = await fetch("/api/game/event?from=" + from + "&to=" + move.cell, {
        signal: AbortSignal.any([controller.signal, AbortSignal.timeout(1800)]),
      });
      if (response.status !== 200 || !firstVisit) return;
      const { event, duration_ms: duration } = await response.json();
      const detail = trailEvents.get(event);
      if (!detail || controller.signal.aborted || path.at(-1) !== move || message) return;
      message = detail.text;
      status.textContent = message;
      if (!reducedMotion.matches) {
        eventAnimation = tiles[move.cell].animate([
          { boxShadow: "0 0 0 0 " + detail.color },
          { boxShadow: "0 0 0 .18rem " + detail.color, offset: .36 },
          { boxShadow: "0 0 0 .34rem transparent" },
        ], { duration, easing: "ease-out" });
      }
    } catch {
      // Walking and scoring continue when the optional event request fails.
    } finally {
      if (eventRequest === controller) eventRequest = undefined;
    }
  }

  function move(next) {
    const { cell, mask } = path.at(-1);
    if (cell === port || !neighbors(cell).includes(next) || reefs.has(next)) return;
    if (next === port && mask !== allBeacons) {
      message = "走访三处风物后即可抵达驿站。";
      render();
      return;
    }
    const bit = beacons.get(next) ?? 0;
    const firstVisit = !path.some(entry => entry.cell === next);
    const entry = { cell: next, mask: mask | bit };
    clearEvent();
    path.push(entry);
    message = bit && !(mask & bit) ? "已走访" + landmarks.get(bit)[1] + "。" : "";
    if (next === port) {
      const length = path.length - 1;
      if (!best || length < best) {
        best = length;
        try { localStorage.setItem(bestKey, String(best)); } catch {}
      }
      message = length === par ? length + " 步走遍山河，已达最短路线。" : length + " 步走完全程。最短路线为 " + par + " 步。";
    }
    render();
    void showEvent(cell, entry, firstVisit);
    board.focus({ preventScroll: true });
  }

  board.addEventListener("keydown", event => {
    if (!Object.hasOwn(directions, event.key)) return;
    event.preventDefault();
    move(step(path.at(-1).cell, directions[event.key]));
  });
  undo.addEventListener("click", () => {
    if (path.length <= 1) return;
    clearEvent();
    path.pop();
    message = "已撤销一步。";
    render();
    board.focus({ preventScroll: true });
  });
  reset.addEventListener("click", () => {
    clearEvent();
    path = [{ cell: start, mask: 0 }];
    message = "";
    render();
    board.focus({ preventScroll: true });
  });
  render();
}

function setupLandscape() {
  const canvas = document.getElementById("river");
  const mist = document.getElementById("mist");
  const ctx = canvas.getContext("2d");
  if (!ctx) return;

  // Map the same two banks onto the viewport, then clip the moving current to them.
  const bank = new Path2D(`M 956 -38
    C 1034 20 768 39 780 105 C 791 162 974 182 906 240
    C 821 311 395 310 301 410 C 252 465 371 514 544 552
    L 1040 552 C 886 497 531 466 526 421 C 517 352 881 331 984 248
    C 1089 164 814 151 819 102 C 823 52 1100 32 1028 -38 Z`);
  const ripples = Array.from({ length: 84 }, (_, i) => {
    const row = Math.floor(i / 4);
    return {
      x: 55 + (i % 4) * 290 + (row * 149 % 185),
      y: 18 + row * 25 + (i * 13 % 9),
      length: 23 + row * 2 + (i * 19 % 45),
      phase: i * 2.39996,
    };
  });
  const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");
  const pointer = { x: 0, y: 0 };
  const offset = { x: 0, y: 0 };
  let fill;
  let frame = 0;
  let lastPaint = 0;
  let elapsed = 0;

  function paint(time) {
    ctx.clearRect(0, 0, 1200, 540);
    ctx.save();
    ctx.translate(offset.x * 13, offset.y * 9);
    ctx.fillStyle = fill;
    ctx.fill(bank);
    ctx.strokeStyle = "rgba(220,181,119,.29)";
    ctx.lineWidth = 1.3;
    ctx.stroke(bank);
    ctx.clip(bank);
    ctx.lineCap = "round";
    ctx.strokeStyle = "#edc58b";
    for (const ripple of ripples) {
      const { x, y, length, phase } = ripple;
      const progress = time * (.35 + y / 1600) + phase;
      const drift = Math.sin(progress) * (7 + y * .025);
      const shimmer = .28 + .17 * Math.cos(progress + offset.x * 1.8);
      ctx.globalAlpha = shimmer;
      ctx.lineWidth = .65 + y / 520;
      ctx.beginPath();
      ctx.moveTo(x + drift, y);
      ctx.quadraticCurveTo(x + drift + length / 2, y + 2 + Math.sin(progress), x + drift + length, y);
      ctx.stroke();
    }
    ctx.restore();
    mist.style.setProperty("--mist-x", offset.x * 24 + "px");
    mist.style.setProperty("--mist-y", offset.y * 13 + "px");
  }

  function resize() {
    const { clientWidth, clientHeight } = canvas;
    const ratio = Math.min(devicePixelRatio, 1.2, 1400 / clientWidth);
    canvas.width = Math.max(1, Math.round(clientWidth * ratio));
    canvas.height = Math.max(1, Math.round(clientHeight * ratio));
    ctx.setTransform(canvas.width / 1200, 0, 0, canvas.height / 540, 0, 0);
    fill = ctx.createLinearGradient(0, 0, 0, 540);
    fill.addColorStop(0, "rgba(193,150,93,.13)");
    fill.addColorStop(.58, "rgba(184,139,83,.34)");
    fill.addColorStop(1, "rgba(143,106,62,.43)");
    paint(reducedMotion.matches ? 0 : elapsed);
  }

  function tick(now) {
    // The water moves slowly, so there is no need to repaint on every display refresh.
    if (!lastPaint || now - lastPaint >= 33) {
      elapsed += lastPaint ? Math.min(now - lastPaint, 70) / 1000 : 0;
      lastPaint = now;
      offset.x += (pointer.x - offset.x) * .12;
      offset.y += (pointer.y - offset.y) * .12;
      paint(elapsed);
    }
    frame = requestAnimationFrame(tick);
  }

  function updateMotion() {
    cancelAnimationFrame(frame);
    lastPaint = 0;
    mist.classList.toggle("paused", document.hidden);
    if (reducedMotion.matches) {
      pointer.x = pointer.y = offset.x = offset.y = 0;
      paint(0);
    } else if (!document.hidden) {
      frame = requestAnimationFrame(tick);
    }
  }

  addEventListener("pointermove", event => {
    if (event.pointerType === "touch" || reducedMotion.matches) return;
    pointer.x = (event.clientX / innerWidth) * 2 - 1;
    pointer.y = (event.clientY / innerHeight) * 2 - 1;
  }, { passive: true });
  document.documentElement.addEventListener("pointerleave", () => { pointer.x = pointer.y = 0; });
  document.addEventListener("visibilitychange", updateMotion);
  reducedMotion.addEventListener("change", updateMotion);
  addEventListener("resize", resize);
  resize();
  updateMotion();
}

void play();
setupLandscape();
