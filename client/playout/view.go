// Copyright 2023 defsub
//
// This file is part of Takeout.
//
// Takeout is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// Takeout is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE.  See the GNU Affero General Public License for
// more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Takeout.  If not, see <https://www.gnu.org/licenses/>.

package playout

import (
	"fmt"
	"github.com/takeoutfm/takeout/client/player"
)

type Viewer interface {
	OnStart(*player.Player)
	OnTrack(*player.Player)
	OnError(*player.Player, error)
	OnStop()
}

type SimpleView struct {
}

func NewSimpleView() Viewer {
	return &SimpleView{}
}

func (SimpleView) OnStart(p *player.Player) {
}

func (SimpleView) OnTrack(p *player.Player) {
	fmt.Printf("%s / %s / %s\n", p.Artist(), p.Album(), p.Title())
}

func (SimpleView) OnError(p *player.Player, err error) {
	fmt.Printf("Error %v\n", err)
}

func (SimpleView) OnStop() {
}
