# Content research, October 2026

Research pass over all 332 entries (significance), the top 100 by that ranking (facts,
catalogue fit, official sites, Google relevance, images; 30 in round one, 70 more in
round two), and recent Amiga news (missing entries). Done 14 Oct 2026 with web
research; every proposal carries its sources.

| File | What it is | How to use it |
|---|---|---|
| `proposals.json` | 142 proposals in the import format: 93 field changes, 32 image replacements, 17 new entries | `/admin/suggestions/import` → upload → review each in the queue (accept, edit & accept, reject) |
| `ranking.md` / `ranking.json` | Proposed significance (1–5) for every entry, with evidence and factual notes | Weights are owner-only, so apply any you agree with on `/admin/entries/<id>`; 48 entries differ from the current weight |

Notes for review:
- Image proposals name the licence in the reason. Roughly a third are manufacturer or project material with no explicit licence (iComp, Apollo, MNT, MorphOS team, Big Book of Amiga Hardware scans, Epsilon's World); the reason text says which, and the rest are Wikimedia Commons, public-domain or GitHub repo images. Six candidate images were rejected during verification for quality (cluttered photos, a tiny logo, a web-simulator screenshot).
- New entries carry a suggested weight in the reason; set it on Accept.
- The rankers' "Factual notes" section in `ranking.md` lists year/name doubts outside the top 30 (e.g. Buddha IDE 1998 not 2017, MagicWB ~1996, NetSurf Amiga port ~2008, AmiWest since 1998) that were not turned into proposals; they are leads, not verified facts.

## Urgent link fixes found in round two

- `icaros`: icarosdesktop.org has been taken over by an unrelated spam page; the proposal points to the project blog.
- `amigagcc`: the GitHub repo is gone (404); the proposal points to the maintainer's Codeberg mirror.
- `aros` / `aros68k`: aros.org serves a mismatched certificate; the proposal uses aros.sourceforge.io.
- `eabfs`, `sps`, `poseidon`: original sites are placeholders now; proposals give the current homes.

## Essentials revisit (weight 5)

Essentials is the weight-5 set shown by the "Essentials only" switch. The evidence pass scores 29 entries at 5;
the current set has 39. Recommendation, applied by hand on `/admin/entries/<id>` since weight is owner-only:

| Action | Entries | Why |
|---|---|---|
| Add (4 → 5) | `a4000`, `os2x`, `p96` | Flagship AGA machine; the 2.x release that defined the modern Workbench; the RTG system everything else builds on |
| Remove (5 → 4) | `amikit`, `amigavision`, `rtg`, `wayfarer`, `amigadebug`, `scorpion`, `newgames`, `v4sa`, `pistorm32` | Important, but each is one product within a family already represented by an essential (AmiKit/AmigaVision by WHDLoad and Amiga Forever; RTG by P96; V4SA by the Vampire line; PiStorm32 by PiStorm; the dev tools and new-games scene by their parents) |
| Remove (5 → 3) | `morphos320` | A point release; `morphos3` carries the line |
| Keep at 5 despite a 4 score | `minimig`, `os41fe`, `morphos3` | Editorial call: the origin of every FPGA Amiga, and the two living next-generation OS lines; dropping them would leave the Essentials view without the NG side of the story |

Result: 32 essentials. The "newgames" removal is the closest call: keep it if the Essentials view should show the current creative scene.
