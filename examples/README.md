# Examples

## Self-host dogfood workflow

This repository includes a local starter pack for "rules about writing rules":

- Pack: `examples/packs/rule-authoring`
- Consumer: `examples/selfhost/rulepack.json`

Run it:

```bash
cd examples/selfhost
rulepack install
rulepack build
```

Expected outputs are written under `examples/selfhost`:

- `.cursor/rules/`
- `.github/copilot-instructions.md`
- `.codex/rules.md`

To test local drift detection:

1. Edit a module in `examples/packs/rule-authoring/modules/authoring/`.
2. Run `rulepack build` again in `examples/selfhost`.
3. Build should fail until you run `rulepack install` to refresh lock hash.
