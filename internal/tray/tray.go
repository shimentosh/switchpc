// Package tray provides system tray functionality using getlantern/systray.
package tray

import (
	"runtime"
	"sync"

	"github.com/getlantern/systray"

	"vkvm/internal/brand"
)

// maxProfileSlots bounds the profile entries; systray cannot remove items, so
// slots are created up front and shown or hidden as profiles change.
const maxProfileSlots = 8

// MenuItem represents a menu item
type MenuItem struct {
	ID       int
	Title    string
	Callback func()
	item     *systray.MenuItem
	profiles bool // placeholder for the profile slots
}

// Tray manages the system tray icon and menu
type Tray struct {
	tooltip   string
	items     []*MenuItem
	readyCh   chan struct{}
	quitCh    chan struct{}
	onProfile func(name string)

	mu      sync.Mutex
	slots   []*systray.MenuItem
	names   []string
	current string
	pending bool
}

// New creates a new system tray
func New(tooltip string) *Tray {
	return &Tray{
		tooltip: tooltip,
		items:   make([]*MenuItem, 0),
		readyCh: make(chan struct{}),
		quitCh:  make(chan struct{}),
	}
}

// AddMenuItem adds a menu item to the tray
func (t *Tray) AddMenuItem(title string, callback func()) int {
	id := len(t.items)
	t.items = append(t.items, &MenuItem{ID: id, Title: title, Callback: callback})
	return id
}

// AddSeparator adds a separator to the menu
func (t *Tray) AddSeparator() {
	t.items = append(t.items, nil) // nil indicates separator
}

// AddProfiles reserves the place in the menu where "Switch to <profile>"
// entries appear. onClick receives the profile name.
func (t *Tray) AddProfiles(onClick func(name string)) {
	t.onProfile = onClick
	t.items = append(t.items, &MenuItem{ID: len(t.items), profiles: true})
}

// SetProfiles updates the profile entries and marks the current one. It can
// be called at any time, before or after the tray is ready.
func (t *Tray) SetProfiles(names []string, current string) {
	t.mu.Lock()
	t.names = append([]string(nil), names...)
	t.current = current
	ready := t.slots != nil
	t.pending = !ready
	t.mu.Unlock()
	if ready {
		t.applyProfiles()
	}
}

func (t *Tray) applyProfiles() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, slot := range t.slots {
		if i < len(t.names) {
			slot.SetTitle("Switch to " + t.names[i])
			if t.names[i] == t.current {
				slot.Check()
			} else {
				slot.Uncheck()
			}
			slot.Show()
		} else {
			slot.Hide()
		}
	}
}

// SetItemChecked sets the checked state of a menu item
func (t *Tray) SetItemChecked(id int, checked bool) {
	if id >= 0 && id < len(t.items) && t.items[id] != nil && t.items[id].item != nil {
		if checked {
			t.items[id].item.Check()
		} else {
			t.items[id].item.Uncheck()
		}
	}
}

// Run starts the tray event loop (blocks)
func (t *Tray) Run() {
	systray.Run(t.setupMenu, func() { close(t.quitCh) })
}

// setupMenu is called when systray is ready
func (t *Tray) setupMenu() {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(brand.PNG(36, brand.Template), brand.PNG(36, brand.Template))
	} else {
		systray.SetIcon(brand.ICO(brand.App, 16, 24, 32, 48))
	}
	systray.SetTooltip(t.tooltip)
	close(t.readyCh)

	for _, menuItem := range t.items {
		switch {
		case menuItem == nil:
			systray.AddSeparator()
		case menuItem.profiles:
			slots := make([]*systray.MenuItem, maxProfileSlots)
			for i := range slots {
				slots[i] = systray.AddMenuItemCheckbox("", "", false)
				slots[i].Hide()
				go t.watchSlot(i, slots[i])
			}
			t.mu.Lock()
			t.slots = slots
			t.mu.Unlock()
		default:
			menuItem.item = systray.AddMenuItem(menuItem.Title, "")
			if menuItem.Callback != nil {
				go func(mi *MenuItem) {
					for {
						select {
						case <-mi.item.ClickedCh:
							mi.Callback()
						case <-t.quitCh:
							return
						}
					}
				}(menuItem)
			}
		}
	}
	t.applyProfiles()
}

func (t *Tray) watchSlot(index int, slot *systray.MenuItem) {
	for {
		select {
		case <-slot.ClickedCh:
			t.mu.Lock()
			name := ""
			if index < len(t.names) {
				name = t.names[index]
			}
			t.mu.Unlock()
			if name != "" && t.onProfile != nil {
				t.onProfile(name)
			}
		case <-t.quitCh:
			return
		}
	}
}

// Stop stops the tray
func (t *Tray) Stop() {
	systray.Quit()
}
