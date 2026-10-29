package view

// NamedObject 只给现成数据对象命名，不复制或派生对象字段。
type NamedObject struct {
	name  string
	value any
}

// Object 声明模板读取一个现成对象时使用的名字。
func Object(name string, value any) NamedObject {
	if name == "" {
		panic("view.Object: 对象名不能为空")
	}
	return NamedObject{name: name, value: value}
}

// Objects 将独立数据对象交给既有渲染出口；禁止在此取数或加工展示字段。
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
