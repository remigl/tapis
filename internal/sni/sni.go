// Package sni shows Tapis' icon in the Linux system tray using the
// StatusNotifierItem protocol (KDE Plasma, XFCE, Cinnamon, MATE, LXQt,
// Budgie, and GNOME with the "AppIndicator" extension), with its menu
// exported through com.canonical.dbusmenu.
package sni

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"sync"

	"github.com/remigl/tapis/internal/dbus"
)

const (
	itemPath  = "/StatusNotifierItem"
	menuPath  = "/MenuBar"
	itemIface = "org.kde.StatusNotifierItem"
	menuIface = "com.canonical.dbusmenu"
	propIface = "org.freedesktop.DBus.Properties"
	watcher   = "org.kde.StatusNotifierWatcher"
)

// Item is a menu entry.
type Item struct {
	Label    string
	Disabled bool
	Toggle   bool // shows a check box
	Checked  bool
	Sep      bool
	OnClick  func()
}

// Options configure the tray icon.
type Options struct {
	Title    string
	IconPNGs [][]byte      // several sizes of the same icon
	Tooltip  func() string // text under the title
	Menu     func() []Item // rebuilt each time the menu opens
	Activate func()        // left click
}

// Tray is a running tray icon.
type Tray struct {
	opts     Options
	conn     *dbus.Conn
	mu       sync.Mutex
	items    []Item
	revision uint32
	pixmaps  []pixmap
	name     string
}

type pixmap struct {
	w, h int32
	argb []byte
}

// Start connects to the session bus and registers the icon. It returns an
// error if no tray is available.
func Start(opts Options) (*Tray, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	t := &Tray{opts: opts, conn: conn}
	for _, b := range opts.IconPNGs {
		if p, err := toPixmap(b); err == nil {
			t.pixmaps = append(t.pixmaps, p)
		}
	}
	conn.Export(itemPath, t.handleItem)
	conn.Export(menuPath, t.handleMenu)
	t.name = fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	var e dbus.Encoder
	e.String(t.name)
	e.Uint32(4) // DBUS_NAME_FLAG_DO_NOT_QUEUE
	conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName", "su", e.Bytes())
	// Re-register if the panel restarts.
	var m dbus.Encoder
	m.String("type='signal',sender='org.freedesktop.DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='" + watcher + "'")
	conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "AddMatch", "s", m.Bytes())
	conn.OnSignal(func(msg *dbus.Message) {
		if msg.Member == "NameOwnerChanged" && len(msg.Body) == 3 && msg.Body[2] != "" {
			t.register()
		}
	})
	if err := t.register(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("no system tray available: %w", err)
	}
	return t, nil
}

func (t *Tray) register() error {
	var e dbus.Encoder
	e.String(t.name)
	_, err := t.conn.Call(watcher, "/StatusNotifierWatcher", watcher, "RegisterStatusNotifierItem", "s", e.Bytes())
	return err
}

// Close removes the icon.
func (t *Tray) Close() { t.conn.Close() }

// Refresh tells the tray that the tooltip and menu changed.
func (t *Tray) Refresh() {
	t.conn.Emit(itemPath, itemIface, "NewToolTip", "", nil)
	t.mu.Lock()
	t.revision++
	rev := t.revision
	t.mu.Unlock()
	var e dbus.Encoder
	e.Uint32(rev)
	e.Int32(0)
	t.conn.Emit(menuPath, menuIface, "LayoutUpdated", "ui", e.Bytes())
}

func toPixmap(b []byte) (pixmap, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return pixmap{}, err
	}
	r := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, r.Min, draw.Src)
	out := make([]byte, 0, 4*r.Dx()*r.Dy())
	for i := 0; i < len(rgba.Pix); i += 4 { // ARGB32, network byte order
		out = append(out, rgba.Pix[i+3], rgba.Pix[i], rgba.Pix[i+1], rgba.Pix[i+2])
	}
	return pixmap{int32(r.Dx()), int32(r.Dy()), out}, nil
}

// ---- StatusNotifierItem ----

func (t *Tray) writePixmaps(e *dbus.Encoder) {
	e.Array(8, func() {
		for _, p := range t.pixmaps {
			e.Struct(func() {
				e.Int32(p.w)
				e.Int32(p.h)
				e.Array(1, func() {
					for _, b := range p.argb {
						e.Byte(b)
					}
				})
			})
		}
	})
}

func (t *Tray) itemProp(e *dbus.Encoder, name string) bool {
	str := func(s string) { e.Variant("s", func() { e.String(s) }) }
	switch name {
	case "Category":
		str("ApplicationStatus")
	case "Id":
		str("tapis")
	case "Title":
		str(t.opts.Title)
	case "Status":
		str("Active")
	case "IconName", "OverlayIconName", "AttentionIconName", "AttentionMovieName", "IconThemePath":
		str("")
	case "WindowId":
		e.Variant("i", func() { e.Int32(0) })
	case "ItemIsMenu":
		e.Variant("b", func() { e.Bool(false) })
	case "Menu":
		e.Variant("o", func() { e.ObjectPath(menuPath) })
	case "IconPixmap":
		e.Variant("a(iiay)", func() { t.writePixmaps(e) })
	case "OverlayIconPixmap", "AttentionIconPixmap":
		e.Variant("a(iiay)", func() { e.Array(8, func() {}) })
	case "ToolTip":
		tip := ""
		if t.opts.Tooltip != nil {
			tip = t.opts.Tooltip()
		}
		e.Variant("(sa(iiay)ss)", func() {
			e.Struct(func() {
				e.String("")
				t.writePixmaps(e)
				e.String(t.opts.Title)
				e.String(tip)
			})
		})
	default:
		return false
	}
	return true
}

var itemProps = []string{"Category", "Id", "Title", "Status", "WindowId", "IconName", "IconPixmap",
	"OverlayIconName", "OverlayIconPixmap", "AttentionIconName", "AttentionIconPixmap", "AttentionMovieName",
	"ToolTip", "ItemIsMenu", "Menu", "IconThemePath"}

const introspectItem = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node><interface name="org.kde.StatusNotifierItem">
<method name="Activate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="SecondaryActivate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="ContextMenu"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="Scroll"><arg type="i" direction="in"/><arg type="s" direction="in"/></method>
<signal name="NewToolTip"/><signal name="NewIcon"/><signal name="NewStatus"><arg type="s"/></signal>
<property name="Category" type="s" access="read"/><property name="Id" type="s" access="read"/>
<property name="Title" type="s" access="read"/><property name="Status" type="s" access="read"/>
<property name="WindowId" type="i" access="read"/><property name="IconName" type="s" access="read"/>
<property name="IconPixmap" type="a(iiay)" access="read"/><property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
<property name="ItemIsMenu" type="b" access="read"/><property name="Menu" type="o" access="read"/>
</interface></node>`

func (t *Tray) handleItem(m *dbus.Message) (string, []byte, string) {
	var e dbus.Encoder
	switch m.Interface + "." + m.Member {
	case propIface + ".Get":
		if len(m.Body) == 2 {
			if name, _ := m.Body[1].(string); t.itemProp(&e, name) {
				return "v", e.Bytes(), ""
			}
		}
		return "", nil, "org.freedesktop.DBus.Error.UnknownProperty"
	case propIface + ".GetAll":
		e.Array(8, func() {
			for _, p := range itemProps {
				e.Struct(func() { e.String(p); t.itemProp(&e, p) })
			}
		})
		return "a{sv}", e.Bytes(), ""
	case "org.freedesktop.DBus.Introspectable.Introspect":
		e.String(introspectItem)
		return "s", e.Bytes(), ""
	case itemIface + ".Activate":
		if t.opts.Activate != nil {
			go t.opts.Activate()
		}
		return "", nil, ""
	case itemIface + ".SecondaryActivate", itemIface + ".ContextMenu", itemIface + ".Scroll":
		return "", nil, ""
	}
	return "", nil, "org.freedesktop.DBus.Error.UnknownMethod"
}

// ---- dbusmenu ----

func (t *Tray) rebuild() {
	var items []Item
	if t.opts.Menu != nil {
		items = t.opts.Menu()
	}
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
}

func (t *Tray) writeProps(e *dbus.Encoder, it *Item, root bool) {
	e.Array(8, func() {
		kv := func(k, sig string, fill func()) { e.Struct(func() { e.String(k); e.Variant(sig, fill) }) }
		if root {
			kv("children-display", "s", func() { e.String("submenu") })
			return
		}
		if it.Sep {
			kv("type", "s", func() { e.String("separator") })
			return
		}
		kv("label", "s", func() { e.String(it.Label) })
		kv("enabled", "b", func() { e.Bool(!it.Disabled) })
		if it.Toggle {
			kv("toggle-type", "s", func() { e.String("checkmark") })
			state := int32(0)
			if it.Checked {
				state = 1
			}
			kv("toggle-state", "i", func() { e.Int32(state) })
		}
	})
}

// Item IDs are 1..n (index + 1); 0 is the root.
func (t *Tray) writeLayout(e *dbus.Encoder) {
	t.mu.Lock()
	items := append([]Item(nil), t.items...)
	t.mu.Unlock()
	e.Struct(func() {
		e.Int32(0)
		t.writeProps(e, nil, true)
		e.Array(1, func() {
			for i := range items {
				e.Variant("(ia{sv}av)", func() {
					e.Struct(func() {
						e.Int32(int32(i + 1))
						t.writeProps(e, &items[i], false)
						e.Array(1, func() {})
					})
				})
			}
		})
	})
}

func (t *Tray) click(id int32) {
	t.mu.Lock()
	var fn func()
	if id >= 1 && int(id) <= len(t.items) {
		it := t.items[id-1]
		if !it.Disabled && !it.Sep {
			fn = it.OnClick
		}
	}
	t.mu.Unlock()
	if fn != nil {
		go func() {
			fn()
			t.Refresh()
		}()
	}
}

func (t *Tray) handleMenu(m *dbus.Message) (string, []byte, string) {
	var e dbus.Encoder
	switch m.Interface + "." + m.Member {
	case menuIface + ".GetLayout":
		t.rebuild()
		t.mu.Lock()
		rev := t.revision
		t.mu.Unlock()
		e.Uint32(rev)
		t.writeLayout(&e)
		return "u(ia{sv}av)", e.Bytes(), ""
	case menuIface + ".GetGroupProperties":
		t.mu.Lock()
		items := append([]Item(nil), t.items...)
		t.mu.Unlock()
		var ids []any
		if len(m.Body) > 0 {
			ids, _ = m.Body[0].([]any)
		}
		e.Array(8, func() {
			for _, v := range ids {
				id, _ := v.(int32)
				if id == 0 {
					e.Struct(func() { e.Int32(0); t.writeProps(&e, nil, true) })
				} else if id >= 1 && int(id) <= len(items) {
					e.Struct(func() { e.Int32(id); t.writeProps(&e, &items[id-1], false) })
				}
			}
		})
		return "a(ia{sv})", e.Bytes(), ""
	case menuIface + ".Event":
		if len(m.Body) >= 2 {
			id, _ := m.Body[0].(int32)
			if ev, _ := m.Body[1].(string); ev == "clicked" {
				t.click(id)
			}
		}
		return "", nil, ""
	case menuIface + ".EventGroup":
		if len(m.Body) == 1 {
			evs, _ := m.Body[0].([]any)
			for _, ev := range evs {
				f, _ := ev.([]any)
				if len(f) >= 2 && f[1] == "clicked" {
					id, _ := f[0].(int32)
					t.click(id)
				}
			}
		}
		e.Array(4, func() {})
		return "ai", e.Bytes(), ""
	case menuIface + ".AboutToShow":
		t.rebuild()
		e.Bool(false)
		return "b", e.Bytes(), ""
	case menuIface + ".AboutToShowGroup":
		t.rebuild()
		e.Array(4, func() {})
		e.Array(4, func() {})
		return "aiai", e.Bytes(), ""
	case propIface + ".Get", propIface + ".GetAll":
		prop := func(name string) bool {
			switch name {
			case "Version":
				e.Variant("u", func() { e.Uint32(3) })
			case "TextDirection":
				e.Variant("s", func() { e.String("ltr") })
			case "Status":
				e.Variant("s", func() { e.String("normal") })
			case "IconThemePath":
				e.Variant("as", func() { e.Array(4, func() {}) })
			default:
				return false
			}
			return true
		}
		if m.Member == "Get" {
			if len(m.Body) == 2 {
				if name, _ := m.Body[1].(string); prop(name) {
					return "v", e.Bytes(), ""
				}
			}
			return "", nil, "org.freedesktop.DBus.Error.UnknownProperty"
		}
		e.Array(8, func() {
			for _, n := range []string{"Version", "TextDirection", "Status", "IconThemePath"} {
				e.Struct(func() { e.String(n); prop(n) })
			}
		})
		return "a{sv}", e.Bytes(), ""
	case "org.freedesktop.DBus.Introspectable.Introspect":
		e.String(`<node><interface name="com.canonical.dbusmenu"><method name="GetLayout"><arg type="i" direction="in"/><arg type="i" direction="in"/><arg type="as" direction="in"/><arg type="u" direction="out"/><arg type="(ia{sv}av)" direction="out"/></method><method name="Event"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/><arg type="u" direction="in"/></method><method name="AboutToShow"><arg type="i" direction="in"/><arg type="b" direction="out"/></method><signal name="LayoutUpdated"><arg type="u"/><arg type="i"/></signal></interface></node>`)
		return "s", e.Bytes(), ""
	}
	return "", nil, "org.freedesktop.DBus.Error.UnknownMethod"
}
