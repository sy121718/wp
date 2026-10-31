package view

type NamedObject struct {
	name  string
	value any
}

func Object(name string, value any) NamedObject {
	if name == "" {
		panic("view.Object: 对象名不能为空")
	}
	return NamedObject{name: name, value: value}
}

func Objects(objects ...NamedObject) map[string]any {
	values := make(map[string]any, len(objects))
	for _, object := range objects {
		if _, exists := values[object.name]; exists {
			panic("view.Objects: 重复对象名: " + object.name)
		}
		values[object.name] = object.value
	}
	return values
}
