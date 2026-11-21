import { useMemo, useState } from "react";
import { Plus, Star, Trash2 } from "lucide-react";
import type { Game } from "../types";
import { GameIcon } from "./GameArt";

type Tab = "all" | "fav" | "installed";

interface Props {
  games: Game[];
  recent: string[];
  query: string;
  selected: string | null;
  optimizingId: string | null;
  onSelect: (id: string) => void;
  onFavorite: (id: string) => void;
  onAddGame: () => void;
  onRemoveGame: (g: Game) => void;
}

export function Library({ games, recent, query, selected, optimizingId, onSelect, onFavorite, onAddGame, onRemoveGame }: Props) {
  const [tab, setTab] = useState<Tab>("all");

  const { top, rest } = useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = games.filter((g) => !q || [g.name, g.genre, g.id, ...(g.aliases ?? [])].some((label) => label.toLowerCase().includes(q)));
    if (tab === "fav") list = list.filter((g) => g.favorite);
    if (tab === "installed") list = list.filter((g) => g.installed || g.running);
    list = [...list].sort((a, b) => a.name.localeCompare(b.name));
    if (q || tab !== "all") return { top: [] as Game[], rest: list };
    // Running games first, then recently used, then A-Z.
    const pinned = new Set<string>([...games.filter((g) => g.running).map((g) => g.id), ...recent.slice(0, 4)]);
    const top = [...pinned].map((id) => list.find((g) => g.id === id)).filter((g): g is Game => !!g);
    return { top, rest: list.filter((g) => !pinned.has(g.id)) };
  }, [games, recent, query, tab]);

  const counts = {
    fav: games.filter((g) => g.favorite).length,
    installed: games.filter((g) => g.installed || g.running).length,
  };

  const row = (g: Game) => (
    <div key={g.id} className="game-item" style={{ position: "relative" }}>
      <button className="game-row" aria-current={selected === g.id} onClick={() => onSelect(g.id)}>
        <GameIcon game={g} size={32} />
        <span className="meta">
          <span className="name" style={{ display: "block" }}>
            {g.name}
          </span>
          <span className="sub">
            {optimizingId === g.id ? (
              <span className="badge-live">● 加速中</span>
            ) : g.running ? (
              <span className="badge-live">● 运行中</span>
            ) : (
              g.genre
            )}
            {g.custom && !/custom/i.test(g.genre) && <span className="tag">自定义</span>}
          </span>
        </span>
        <span style={{ width: g.custom ? 54 : 26, flex: "none" }} />
      </button>
      {g.custom && (
        <button
          className="star remove"
          style={{ position: "absolute", right: 34, top: "50%", transform: "translateY(-50%)" }}
          aria-label={`从游戏库移除 ${g.name}`}
          title="移除自定义游戏"
          onClick={() => onRemoveGame(g)}
        >
          <Trash2 size={14} />
        </button>
      )}
      <button
        className="star"
        style={{ position: "absolute", right: 8, top: "50%", transform: "translateY(-50%)" }}
        aria-pressed={g.favorite}
        aria-label={g.favorite ? `取消收藏 ${g.name}` : `收藏 ${g.name}`}
        onClick={() => onFavorite(g.id)}
      >
        <Star size={15} fill={g.favorite ? "currentColor" : "none"} />
      </button>
    </div>
  );

  return (
    <aside className="library" aria-label="游戏库">
      <div className="library-head">
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div className="section-label">游戏库</div>
          <button className="btn btn-xs btn-ghost" onClick={onAddGame} title="添加列表之外的游戏">
            <Plus size={14} /> 添加游戏
          </button>
        </div>
        <div className="tabs" role="tablist">
          <button role="tab" aria-selected={tab === "all"} onClick={() => setTab("all")}>
            全部
          </button>
          <button role="tab" aria-selected={tab === "fav"} onClick={() => setTab("fav")}>
            收藏{counts.fav ? ` ${counts.fav}` : ""}
          </button>
          <button role="tab" aria-selected={tab === "installed"} onClick={() => setTab("installed")}>
            已安装{counts.installed ? ` ${counts.installed}` : ""}
          </button>
        </div>
      </div>
      <div className="game-list">
        {top.length > 0 && (
          <>
            <div className="group section-label">最近</div>
            {top.map(row)}
            <div className="group section-label">全部游戏</div>
          </>
        )}
        {rest.map(row)}
        {top.length + rest.length === 0 && (
          <div className="empty-list">
            {tab === "fav"
              ? "暂无收藏。将指针移到游戏上，再点击星标。"
              : tab === "installed"
                ? "本机暂未发现受支持的游戏。"
                : `没有找到「${query}」。`}
          </div>
        )}
      </div>
    </aside>
  );
}
