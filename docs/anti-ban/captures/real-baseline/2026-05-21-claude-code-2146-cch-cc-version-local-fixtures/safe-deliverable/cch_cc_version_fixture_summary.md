# Claude Code 2.1.146 CCH + cc_version local fixture summary

- CLI version: `2.1.146`
- Seed: `0x4d659218e32a3268`
- Formula: `cch=xxh64(body_with_cch_00000,0x4d659218e32a3268)&0xFFFFF; cc_version=sha256("59cf53e54c78"+chars+cli_version)[:3]; chars=first non-<system-reminder> user text positions [4,7,20] with 0 fallback`
- Total localhost `/v1/messages?beta=true` requests inspected: `12`
- Billing-attributed requests verified: `8`
- All CCH matched: `True`
- All cc_version matched: `True`

## Request summaries

- `default_attribution_a` req `0`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`dcfc7c109a144972e23b3c3300fe1d62036c681fff312aa8f94f5bef6aa7b67e`, first_user_text_hash=`d8adb66f9915732364edb6bad6a36db633ed580ae32a4edfa75d17ae3c1801e2`
- `default_attribution_a` req `1`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`c002372318a8c2b83bde0ffbc08dbc57a034c56c6cf598caeb1a27cecc33a5c7`, first_user_text_hash=`092dbda7e199983c6ac52fef46c0e963115799f38d69f68f5117c7b13a0b7e4c`
- `default_attribution_a` req `2`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`c638758f2801c8217c8dcbfadd81e942fc3dfee36ce6a219c88ce5e687d3259b`, first_user_text_hash=`d8adb66f9915732364edb6bad6a36db633ed580ae32a4edfa75d17ae3c1801e2`
- `default_attribution_a` req `3`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`10681427ed6a22d8792d0598989006d7ef2d12c0c31c1c51bef877c01c7b21e1`, first_user_text_hash=`092dbda7e199983c6ac52fef46c0e963115799f38d69f68f5117c7b13a0b7e4c`
- `default_attribution_b` req `0`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`d2ce2a7fa109345ede30d8d83d3e039f840dc8c05d357d7e792d6b2044aaa1ad`, first_user_text_hash=`f3b9e8c69c12fcebbf8762fd605b4df9b24aae91f3146e121b8af7184e7a6384`
- `default_attribution_b` req `1`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`a1b66c64e5742baa290e5f82cce306890571a1e5926dd7469b58660ccd39ac77`, first_user_text_hash=`b0d8b5d13ebea9857328530b98e6240c70574f91a357980dc70eff5e8dec0f88`
- `default_attribution_b` req `2`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`b1463c8b3edf5b9c84381c7909f32480a302a54722898380cc92f40e721832dd`, first_user_text_hash=`f3b9e8c69c12fcebbf8762fd605b4df9b24aae91f3146e121b8af7184e7a6384`
- `default_attribution_b` req `3`: billing=`true`, cch_match=`true`, cc_version_match=`true`, text_mode=`skip_system_reminder`, body_sha256=`7b657e111790bdb2aef339fbc4ee0110a41cd6821bed07bdc9a04478ae107af4`, first_user_text_hash=`b0d8b5d13ebea9857328530b98e6240c70574f91a357980dc70eff5e8dec0f88`
- `attribution_off` req `0`: billing=`false`, cch_match=`false`, cc_version_match=`false`, text_mode=`skip_system_reminder`, body_sha256=`622d1f313bfcecc8c75c01a7d6c718a7714ee1bd236ee57534cb16466cf0993e`, first_user_text_hash=`d8adb66f9915732364edb6bad6a36db633ed580ae32a4edfa75d17ae3c1801e2`
- `attribution_off` req `1`: billing=`false`, cch_match=`false`, cc_version_match=`false`, text_mode=`skip_system_reminder`, body_sha256=`f253ba7a04943078afec2db8f62f568c833a92797a605c8101f7448362203a40`, first_user_text_hash=`092dbda7e199983c6ac52fef46c0e963115799f38d69f68f5117c7b13a0b7e4c`
- `attribution_off` req `2`: billing=`false`, cch_match=`false`, cc_version_match=`false`, text_mode=`skip_system_reminder`, body_sha256=`185b3844dc080aeaab3a69b48aa56df5936324825c8c9022045b9d409caa2b6c`, first_user_text_hash=`d8adb66f9915732364edb6bad6a36db633ed580ae32a4edfa75d17ae3c1801e2`
- `attribution_off` req `3`: billing=`false`, cch_match=`false`, cc_version_match=`false`, text_mode=`skip_system_reminder`, body_sha256=`9933ffb7384b20f1afd0674bcdc4625f579e63c760e807f9a8e0ed6d87823dc8`, first_user_text_hash=`092dbda7e199983c6ac52fef46c0e963115799f38d69f68f5117c7b13a0b7e4c`

> Safe deliverable only stores hashes/booleans/summaries. Raw bodies, raw prompts, raw Authorization values, raw account UUIDs, and raw CCH values remain outside the safe deliverable.