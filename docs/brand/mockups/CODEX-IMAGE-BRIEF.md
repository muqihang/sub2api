# 逐梦 Agent 官网配图 — Codex 生图 Brief

> 把这份文档整份发给 Codex。它包含设计背景、调色板、每张图的用途/尺寸/文件名/提示词。
> 生成后的 PNG 放进 `docs/brand/mockups/img/`，文件名按本文命名，首页会自动用上。
> 配套草图(让 Codex 研究排版与氛围)：`docs/brand/mockups/homepage.html`(直接在浏览器打开看)。

---

## 0. 这些图用在哪（给 Codex 的背景）

我们在做一个开发者产品「逐梦 Agent」的官网首页。它是一个**本地网关**，接管 Codex / Claude Code 等顶级 AI 编程工具，让它们跑任意模型。官网是**深色、电影感、午夜极光**的高级气质。配图的作用是**氛围背景**——衬托内容，不能抢戏、不能有文字、不能有具体物体喧宾夺主。整体像高端 SaaS / 金融级产品 + 一点"逐梦/星空"的梦境感。

## 1. 全局调色板（所有图都必须贴合）

```
底色   #080B16  深夜蓝黑（主背景，图要能无缝融进去）
面板   #0E1430  深靛蓝
点缀   #D9B779  香槟金（高级、克制，少量）
极光   violet #7A5CFF → rose #FF6FA3 → gold #D9B779（柔和渐变光带）
暖白   #ECEAE3  （高光）
```

**统一风格关键词（每个 prompt 都带上）：**
`dark cinematic, deep midnight navy-black background #080B16, soft aurora gradients (violet #7A5CFF, rose #FF6FA3, champagne gold #D9B779), premium, subtle, lots of negative space, high-end SaaS aesthetic, no text, no logos, no watermark, no people, soft film grain, 8k, photographic depth`

**统一负面词（negative prompt）：**
`text, letters, words, watermark, logo, ui, buttons, faces, people, hands, harsh neon, cyberpunk cliché, lens flare overload, busy clutter, low-res, jpeg artifacts, oversaturated`

---

## 2. 需要的图（按优先级）

### 🥇 P1 — `aurora-hero.png`（首屏背景，最重要）
- **用途**：整个首页顶部的氛围背景，衬在文字与"本地脊柱"图后面。
- **尺寸**：2400 × 1400（横向，留出顶部 60% 给极光、底部渐隐到纯底色）
- **Prompt**：
  > A dark cinematic aurora over a vast midnight sky, deep navy-black background #080B16, soft flowing aurora ribbons in violet #7A5CFF blending into rose #FF6FA3 and champagne gold #D9B779, very subtle, dreamy, the lower half fading smoothly into solid dark, enormous negative space, premium high-end SaaS hero background, soft film grain, no text, no logos, no people, 8k, photographic
- **要点**：左上偏紫、右上偏玫瑰、中部一抹金；**下半部必须渐隐到接近 #080B16**（否则压不住文字）。整体**低对比、安静**。

### 🥈 P2 — `enterprise-bg.png`（企业区块背景）
- **用途**：企业区块那张深色卡片的氛围背景，更"稳重、机构感"。
- **尺寸**：1600 × 1000
- **Prompt**：
  > Abstract dark architectural atmosphere, deep midnight navy #0A0F22, a faint geometric grid or vault-like structure dissolving into fog, single soft champagne-gold #D9B779 light source from upper right, very subtle violet ambiance, sense of security and scale, premium enterprise, lots of dark negative space, no text, no people, soft grain, 8k
- **要点**：比 hero 更冷静、更"机房/金库/边界"的安全感，金光极少量。

### 🥉 P3 — `og-card.png`（社交分享图 / Open Graph）
- **用途**：发到微信/X 等链接预览图。
- **尺寸**：1200 × 630（**安全留白**，中心区不要太满，后续可能叠 logo 文字）
- **Prompt**：
  > Minimal premium OG card, deep midnight navy-black #080B16, a single softly glowing champagne-gold node at center with faint aurora light threads (violet, rose) radiating outward to small dim points, elegant, lots of negative space, no text, no logo, cinematic, 8k
- **要点**：中心一个发光节点 + 向外的细光线（呼应"本地网关 → 各模型"的脊柱概念），但**抽象、无文字**。

### 选做 P4 — `noise-texture.png`（颗粒/纹理叠层，可选）
- **用途**：全站叠一层极淡纹理增加质感（目前用 CSS 颗粒，可被替换）。
- **尺寸**：1024 × 1024，可平铺
- **Prompt**：
  > Seamless tileable subtle dark noise / fine film grain texture, near-black, very low contrast, faint champagne-gold speckles, premium paper-like grain, no pattern visible, no text

---

## 3. 生成后怎么接上（给我或你自己做）

1. 把 PNG 放到 `docs/brand/mockups/img/`，文件名严格用上面的。
2. 打开 `homepage.html`，找到 `.aurora-img` 这条 CSS，把注释那行启用：
   ```css
   .aurora-img{background-image:url('./img/aurora-hero.png');opacity:.55;mix-blend-mode:screen}
   ```
3. 刷新浏览器即可看到极光照片版背景叠在 CSS 光晕之上。我也可以收到图后直接帮你接好并微调透明度/混合模式。

---

## 4. 给 Codex 的一句话总指令（可直接粘）

> 你是资深视觉设计师。请阅读这份 brief 和我附带的 `homepage.html` 草图，理解它"午夜极光 + 香槟金"的深色高级气质。按 §2 列表生成 P1–P3 三张**无文字的氛围背景图**，严格贴合 §1 调色板与统一风格/负面词。每张都要：下半部/边缘能融进 #080B16 深底，低对比、安静、留白多、不抢内容。先给我 P1 `aurora-hero.png`。
