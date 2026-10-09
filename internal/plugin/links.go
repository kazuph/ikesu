package plugin

import "regexp"

func (e *Engine) InvokeLink(url string, context Context) (bool, error) {
	items, err := e.Store.List("")
	if err != nil {
		return false, err
	}
	for _, owner := range items {
		if !owner.Enabled {
			continue
		}
		manifest, _, err := ReadManifest(owner.ManifestPath, e.Store.Version)
		if err != nil {
			return false, err
		}
		for _, handler := range manifest.LinkHandlers {
			if !supported(effectivePlatforms(handler.Platforms, manifest.Platforms)) {
				continue
			}
			pattern, err := regexp.Compile(handler.Pattern)
			if err != nil {
				return false, err
			}
			if !pattern.MatchString(url) {
				continue
			}
			copyContext := Context{}
			for key, value := range context {
				copyContext[key] = value
			}
			copyContext["invocation_source"] = "link_click"
			copyContext["clicked_url"] = url
			copyContext["link_handler_id"] = handler.ID
			_, _, err = e.Invoke(owner.ID, handler.Action, copyContext)
			return true, err
		}
	}
	return false, nil
}
