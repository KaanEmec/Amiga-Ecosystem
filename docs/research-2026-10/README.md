# Content research, October 2026

Research pass over all 332 entries (significance), the top 250 by that ranking (facts,
catalogue fit, official sites, Google relevance, images; 30 in round one, 70 in
round two, 50 in round three and 100 in round four, 14–15 Oct), and recent Amiga news (missing entries). Done 14 Oct 2026 with web
research; every proposal carries its sources.

| File | What it is | How to use it |
|---|---|---|
| `proposals.json` | 352 proposals in the import format: 232 field changes, 103 image replacements, 17 new entries (720 KB; the import page accepts up to 1 MB) | `/admin/suggestions/import` → upload → review each in the queue (accept, edit & accept, reject) |
| `ranking.md` / `ranking.json` | Proposed significance (1–5) for every entry, with evidence and factual notes | Weights are owner-only, so apply any you agree with on `/admin/entries/<id>`; 48 entries differ from the current weight |

Notes for review:
- Image proposals name the licence in the reason. Roughly a third are manufacturer or project material with no explicit licence (iComp, Apollo, MNT, MorphOS team, Big Book of Amiga Hardware scans, Epsilon's World); the reason text says which, and the rest are Wikimedia Commons, public-domain or GitHub repo images. Thirty-odd candidate images were rejected during verification: cluttered or watermarked photos, images under 500 px, files over the fetcher's 8 MB cap, PDFs or zips, and captures the agent could only make locally.
- New entries carry a suggested weight in the reason; set it on Accept.
- The rankers' "Factual notes" section in `ranking.md` lists year/name doubts outside the top 30 (e.g. Buddha IDE 1998 not 2017, MagicWB ~1996, NetSurf Amiga port ~2008, AmiWest since 1998) that were not turned into proposals; they are leads, not verified facts.

## Urgent link fixes found in round two

- `icaros`: icarosdesktop.org has been taken over by an unrelated spam page; the proposal points to the project blog.
- `amigagcc`: the GitHub repo is gone (404); the proposal points to the maintainer's Codeberg mirror.
- `aros` / `aros68k`: aros.org serves a mismatched certificate; the proposal uses aros.sourceforge.io.
- `eabfs`, `sps`, `poseidon`: original sites are placeholders now; proposals give the current homes.

## Round three notes (ranks 101–150)

- Wrong-board entries: `a500pp` described an open A500 remake but is Bob's Bits' commercial A500 Plus replica; `aa3000` is rewritten from forum summaries only (medium confidence).
- Year corrections with sources: Icedrake 2022, AmiBlitz 2008, AmiWest 1998, AMR 2007, AmigaAMP 1997, REDPILL 2016, ReAmiga 1200 2018, ACE 2016, BetterWB 2011, CoffinOS 2016.
- The four Apple Mac entries credited "community" drivers; MorphOS support was the MorphOS Team's own releases (2.4 to 3.2).
- Unresolved: `x5000-40` year (no source for 2020), `ags2` needs a fresh AGS2 screenshot (its current image shows an unrelated game collection), `arosone` maker credit unconfirmed.

## Round four notes (ranks 151–250)

- Removals: agents were asked to flag entries that are irrelevant, duplicates or unverifiable; none met that bar, so there are no removal proposals. Aggregate entries (`ports`, `podcasts`, `national`, `lemon`, `patches`, `modernfs`) stay, with text corrections.
- Category moves proposed: `aaa` to Graphics & video, `hombre` to next-generation hardware, `emu68` to Emulators, `afaos` to Shells & desktops. `ppaint` is narrowed to Personal Paint alone.
- Live-content problems found: `hyperion2024` still carries an internal editor's note in its description; `amigaosxl` and `powerup` show the wrong software or game; `warp` calls a line "dormant" that CS-Lab sells today; `wicher` and `xsurf` merge different products; `a500pp` and `aa3000` described the wrong boards (round three).
- Dead or moved links fixed: vamiga.net, platon42.de, armiga.com (parked), amistart's old site, roadshow's https certificate, icomp redirects.
- Unresolved years: `roadshow`, `tf3xxx`, `manticore`, `vinced`, `zune`, `x5000-40`.

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
