package prompt

var boilerplateCatalogHintKey = Define[struct{}]("tool_results.boilerplate_catalog_hint", struct{}{})

// BoilerplateCatalogHintText is search_boilerplate_catalog's "hint" field,
// steering an agent to copy the matched starter rather than regenerate it.
func BoilerplateCatalogHintText() string { return Text(boilerplateCatalogHintKey) }
