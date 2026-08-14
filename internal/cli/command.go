package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// 本文件用标准库实现命令树与 flag 解析，替代 github.com/spf13/cobra。
// 动机：内网无法访问公共 Go module 代理，构建时拉不到依赖。
// 只实现本项目实际用到的能力，字段名与 cobra 保持一致以降低改动面。

// ---- Flag ----

type flagKind int

const (
	flagString flagKind = iota
	flagBool
	flagInt
	flagStringArray
)

type flag struct {
	name      string
	shorthand string
	usage     string
	kind      flagKind
	changed   bool

	sVal  *string
	bVal  *bool
	iVal  *int
	saVal *[]string
}

// FlagSet 持有一条命令的所有 flag。
type FlagSet struct {
	flags   []*flag
	byName  map[string]*flag
	byShort map[string]*flag
}

func newFlagSet() *FlagSet {
	return &FlagSet{
		byName:  map[string]*flag{},
		byShort: map[string]*flag{},
	}
}

func (fs *FlagSet) add(f *flag) {
	if fs.byName == nil {
		fs.byName = map[string]*flag{}
		fs.byShort = map[string]*flag{}
	}
	fs.flags = append(fs.flags, f)
	fs.byName[f.name] = f
	if f.shorthand != "" {
		fs.byShort[f.shorthand] = f
	}
}

func (fs *FlagSet) StringVarP(p *string, name, shorthand, value, usage string) {
	*p = value
	fs.add(&flag{name: name, shorthand: shorthand, usage: usage, kind: flagString, sVal: p})
}

func (fs *FlagSet) StringVar(p *string, name, value, usage string) {
	fs.StringVarP(p, name, "", value, usage)
}

func (fs *FlagSet) String(name, value, usage string) *string {
	p := new(string)
	fs.StringVar(p, name, value, usage)
	return p
}

func (fs *FlagSet) BoolVarP(p *bool, name, shorthand string, value bool, usage string) {
	*p = value
	fs.add(&flag{name: name, shorthand: shorthand, usage: usage, kind: flagBool, bVal: p})
}

func (fs *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	fs.BoolVarP(p, name, "", value, usage)
}

func (fs *FlagSet) Bool(name string, value bool, usage string) *bool {
	p := new(bool)
	fs.BoolVar(p, name, value, usage)
	return p
}

func (fs *FlagSet) IntVar(p *int, name string, value int, usage string) {
	*p = value
	fs.add(&flag{name: name, usage: usage, kind: flagInt, iVal: p})
}

func (fs *FlagSet) StringArrayVarP(p *[]string, name, shorthand string, value []string, usage string) {
	*p = value
	fs.add(&flag{name: name, shorthand: shorthand, usage: usage, kind: flagStringArray, saVal: p})
}

func (fs *FlagSet) StringArrayVar(p *[]string, name string, value []string, usage string) {
	fs.StringArrayVarP(p, name, "", value, usage)
}

func (fs *FlagSet) StringArray(name string, value []string, usage string) *[]string {
	p := new([]string)
	fs.StringArrayVar(p, name, value, usage)
	return p
}

// GetString / GetBool / GetStringArray 读取已解析的值。
// 与 cobra 一致返回 (value, error)，但本实现不会产生错误。
func (fs *FlagSet) GetString(name string) (string, error) {
	if f, ok := fs.byName[name]; ok && f.sVal != nil {
		return *f.sVal, nil
	}
	return "", fmt.Errorf("flag 未定义: %s", name)
}

func (fs *FlagSet) GetBool(name string) (bool, error) {
	if f, ok := fs.byName[name]; ok && f.bVal != nil {
		return *f.bVal, nil
	}
	return false, fmt.Errorf("flag 未定义: %s", name)
}

func (fs *FlagSet) GetStringArray(name string) ([]string, error) {
	if f, ok := fs.byName[name]; ok && f.saVal != nil {
		return *f.saVal, nil
	}
	return nil, fmt.Errorf("flag 未定义: %s", name)
}

// Changed 报告该 flag 是否在命令行中被显式指定。
func (fs *FlagSet) Changed(name string) bool {
	if f, ok := fs.byName[name]; ok {
		return f.changed
	}
	return false
}

func (f *flag) set(raw string) error {
	switch f.kind {
	case flagString:
		*f.sVal = raw
	case flagBool:
		if raw == "" {
			*f.bVal = true
		} else {
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("--%s 需要布尔值: %v", f.name, err)
			}
			*f.bVal = b
		}
	case flagInt:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("--%s 需要整数: %v", f.name, err)
		}
		*f.iVal = n
	case flagStringArray:
		*f.saVal = append(*f.saVal, raw)
	}
	f.changed = true
	return nil
}

// needsValue 报告该 flag 是否需要跟一个值。布尔 flag 不需要。
func (f *flag) needsValue() bool { return f.kind != flagBool }

// ---- Args 校验 ----

// PositionalArgs 校验位置参数个数。
type PositionalArgs func(cmd *Command, args []string) error

// NoArgs 不接受任何位置参数。
func NoArgs(cmd *Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("命令 %q 不接受位置参数，收到 %d 个", cmd.Name(), len(args))
	}
	return nil
}

// ExactArgs 要求恰好 n 个位置参数。
func ExactArgs(n int) PositionalArgs {
	return func(cmd *Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("命令 %q 需要 %d 个位置参数，收到 %d 个", cmd.Name(), n, len(args))
		}
		return nil
	}
}

// ---- Command ----

// Command 是命令树节点。字段名与 cobra 对齐。
type Command struct {
	Use     string // "name" 或 "name <arg1> <arg2>"
	Short   string
	Long    string
	Example string
	Args    PositionalArgs
	RunE    func(cmd *Command, args []string) error

	SilenceUsage  bool
	SilenceErrors bool

	children []*Command
	parent   *Command

	flags           *FlagSet
	persistentFlags *FlagSet
}

// Name 返回命令名（Use 的第一个词）。
func (c *Command) Name() string {
	if c.Use == "" {
		return ""
	}
	return strings.Fields(c.Use)[0]
}

// Flags 返回本命令的 flag 集合。
func (c *Command) Flags() *FlagSet {
	if c.flags == nil {
		c.flags = newFlagSet()
	}
	return c.flags
}

// PersistentFlags 返回会被子命令继承的 flag 集合。
func (c *Command) PersistentFlags() *FlagSet {
	if c.persistentFlags == nil {
		c.persistentFlags = newFlagSet()
	}
	return c.persistentFlags
}

// AddCommand 挂载子命令。
func (c *Command) AddCommand(cmds ...*Command) {
	for _, sub := range cmds {
		sub.parent = c
		c.children = append(c.children, sub)
	}
}

// find 按名字查找直接子命令。
func (c *Command) find(name string) *Command {
	for _, sub := range c.children {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

// commandPath 返回从根到本命令的完整路径，用于帮助文本。
func (c *Command) commandPath() string {
	if c.parent == nil {
		return c.Name()
	}
	return c.parent.commandPath() + " " + c.Name()
}

// lookupFlag 在本命令及所有祖先的 persistent flags 中查找。
func (c *Command) lookupFlag(name string, short bool) *flag {
	for node := c; node != nil; node = node.parent {
		if node.flags != nil {
			var f *flag
			var ok bool
			if short {
				f, ok = node.flags.byShort[name]
			} else {
				f, ok = node.flags.byName[name]
			}
			// 非本命令的普通 flag 不参与继承
			if ok && node == c {
				return f
			}
		}
		if node.persistentFlags != nil {
			if short {
				if f, ok := node.persistentFlags.byShort[name]; ok {
					return f
				}
			} else {
				if f, ok := node.persistentFlags.byName[name]; ok {
					return f
				}
			}
		}
	}
	return nil
}

// Execute 从 os.Args 解析并执行。
func (c *Command) Execute() error {
	return c.executeArgs(os.Args[1:])
}

func (c *Command) executeArgs(args []string) error {
	cmd, rest, err := c.traverse(args)
	if err != nil {
		return err
	}

	positional, wantHelp, err := cmd.parseFlags(rest)
	if err != nil {
		return err
	}
	if wantHelp {
		cmd.printHelp()
		return nil
	}

	// 没有 RunE 的节点（如分组）直接打印帮助
	if cmd.RunE == nil {
		cmd.printHelp()
		return nil
	}

	if cmd.Args != nil {
		if err := cmd.Args(cmd, positional); err != nil {
			return err
		}
	}
	return cmd.RunE(cmd, positional)
}

// traverse 沿命令树下钻，返回最终命令与剩余参数。
// 遇到以 - 开头的参数即停止下钻。
func (c *Command) traverse(args []string) (*Command, []string, error) {
	cur := c
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			break
		}
		sub := cur.find(a)
		if sub == nil {
			break
		}
		cur = sub
		i++
	}
	return cur, args[i:], nil
}

// parseFlags 解析 flag 与位置参数。
// 支持 --name=value、--name value、-x value、-x（布尔）、以及 -- 之后全部视为位置参数。
func (c *Command) parseFlags(args []string) (positional []string, wantHelp bool, err error) {
	i := 0
	for i < len(args) {
		a := args[i]

		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if a == "-h" || a == "--help" {
			return nil, true, nil
		}

		switch {
		case strings.HasPrefix(a, "--"):
			body := a[2:]
			name, val, hasVal := strings.Cut(body, "=")
			f := c.lookupFlag(name, false)
			if f == nil {
				return nil, false, fmt.Errorf("未知参数: --%s", name)
			}
			if !hasVal && f.needsValue() {
				if i+1 >= len(args) {
					return nil, false, fmt.Errorf("参数 --%s 缺少值", name)
				}
				val = args[i+1]
				i++
			}
			if err := f.set(val); err != nil {
				return nil, false, err
			}
			i++

		case len(a) > 1 && a[0] == '-':
			name := a[1:]
			var val string
			var hasVal bool
			if idx := strings.Index(name, "="); idx >= 0 {
				name, val, hasVal = name[:idx], name[idx+1:], true
			}
			f := c.lookupFlag(name, true)
			if f == nil {
				return nil, false, fmt.Errorf("未知参数: -%s", name)
			}
			if !hasVal && f.needsValue() {
				if i+1 >= len(args) {
					return nil, false, fmt.Errorf("参数 -%s 缺少值", name)
				}
				val = args[i+1]
				i++
			}
			if err := f.set(val); err != nil {
				return nil, false, err
			}
			i++

		default:
			positional = append(positional, a)
			i++
		}
	}
	return positional, false, nil
}

// ---- 帮助输出 ----

func (c *Command) printHelp() {
	var b strings.Builder

	if c.Long != "" {
		b.WriteString(c.Long + "\n\n")
	} else if c.Short != "" {
		b.WriteString(c.Short + "\n\n")
	}

	b.WriteString("Usage:\n  " + c.usageLine() + "\n")

	if len(c.children) > 0 {
		b.WriteString("\nAvailable Commands:\n")
		names := make([]string, 0, len(c.children))
		byName := map[string]*Command{}
		width := 0
		for _, sub := range c.children {
			names = append(names, sub.Name())
			byName[sub.Name()] = sub
			if len(sub.Name()) > width {
				width = len(sub.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			b.WriteString(fmt.Sprintf("  %-*s  %s\n", width, n, byName[n].Short))
		}
	}

	if c.flags != nil && len(c.flags.flags) > 0 {
		b.WriteString("\nFlags:\n")
		b.WriteString(formatFlags(c.flags))
	}

	// 汇总所有祖先的 persistent flags
	var inherited []*flag
	for node := c; node != nil; node = node.parent {
		if node.persistentFlags != nil {
			inherited = append(inherited, node.persistentFlags.flags...)
		}
	}
	if len(inherited) > 0 {
		label := "\nGlobal Flags:\n"
		if c.parent == nil {
			label = "\nFlags:\n"
		}
		b.WriteString(label)
		b.WriteString(formatFlagList(inherited))
	}

	if c.Example != "" {
		b.WriteString("\nExamples:\n" + c.Example + "\n")
	}

	if len(c.children) > 0 {
		b.WriteString("\n使用 \"" + c.commandPath() + " <command> --help\" 查看子命令详情。\n")
	}

	fmt.Fprint(os.Stdout, b.String())
}

func (c *Command) usageLine() string {
	line := c.commandPath()
	if fields := strings.Fields(c.Use); len(fields) > 1 {
		line += " " + strings.Join(fields[1:], " ")
	}
	if len(c.children) > 0 {
		line += " <command>"
	}
	if (c.flags != nil && len(c.flags.flags) > 0) || c.parent == nil {
		line += " [flags]"
	}
	return line
}

func formatFlags(fs *FlagSet) string { return formatFlagList(fs.flags) }

func formatFlagList(flags []*flag) string {
	type row struct{ left, usage string }
	rows := make([]row, 0, len(flags))
	width := 0
	for _, f := range flags {
		left := "      --" + f.name
		if f.shorthand != "" {
			left = "  -" + f.shorthand + ", --" + f.name
		}
		switch f.kind {
		case flagString:
			left += " string"
		case flagInt:
			left += " int"
		case flagStringArray:
			left += " strings"
		}
		if len(left) > width {
			width = len(left)
		}
		rows = append(rows, row{left, f.usage})
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%-*s   %s\n", width, r.left, r.usage))
	}
	return b.String()
}
