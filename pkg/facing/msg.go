package facing

type Msg struct {
	Key      string
	Fallback string
	Args     []string
	Named    map[string]string
}

func (m Msg) WithArgs(args ...string) Msg {
	m.Args = args
	return m
}

func (m Msg) WithNamed(params map[string]string) Msg {
	m.Named = params
	return m
}

func (m Msg) Empty() bool {
	return m.Key == "" && m.Fallback == ""
}
