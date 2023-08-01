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
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/takeoutfm/takeout/client/api"
	"github.com/takeoutfm/takeout/client/player"
	"github.com/takeoutfm/takeout/lib/spiff"

	"github.com/takeoutfm/takeout/music"
	"github.com/takeoutfm/takeout/progress"
)

type PlayOptions struct {
	Query   string
	Radio   string
	Repeat  bool
	Shuffle bool
	Simple  bool
	Stream  string
	Visual  bool
}

func (playout Playout) Play(options PlayOptions) error {
	var view Viewer

	if options.Visual {
		view = NewVisualView()
	} else {
		view = NewSimpleView()
	}

	result, err := api.Progress(playout)
	if err != nil {
		return err
	}
	offsets := make(map[string]progress.Offset)
	for _, o := range result.Offsets {
		offsets[o.ETag] = o
	}

	var playlist *spiff.Playlist

	if len(options.Stream) > 0 || len(options.Radio) > 0 {
		result, err := api.Radio(playout)
		if err != nil {
			return err
		}

		var name, spiffType string
		var list []music.Station
		if len(options.Stream) > 0 {
			name = options.Stream
			spiffType = spiff.TypeStream
			list = append(list, result.Stream...)
		} else {
			name = options.Radio
			spiffType = spiff.TypeMusic
			list = append(list, result.Artist...)
			list = append(list, result.Genre...)
			list = append(list, result.Other...)
			list = append(list, result.Period...)
			list = append(list, result.Series...)
			list = append(list, result.Similar...)
		}

		for _, s := range list {
			if strings.EqualFold(s.Name, name) {
				ref := fmt.Sprintf("/music/radio/stations/%d", s.ID)
				playlist, err = api.Replace(playout, ref,
					spiffType, s.Creator, s.Name)
				if err != nil {
					return err
				}
				break
			}
		}
		if playlist == nil {
			return fmt.Errorf("radio/stream not found")
		}
	}

	if playlist == nil {
		if len(options.Query) > 0 {
			playlist, err = api.SearchReplace(playout, options.Query, options.Shuffle)
		} else {
			playlist, err = api.Playlist(playout)
		}
	}
	if err != nil {
		return err
	}

	onTrack := func(p *player.Player) {
		view.OnTrack(p)
		if p.IsStream() == false {
			go func() {
				api.Position(playout, p.Index(), 0)
			}()
		}
	}

	onPause := func(p *player.Player) {
		if p.IsStream() == false {
			go func() {
				pos, _ := p.Position()
				api.Position(playout, p.Index(), pos.Seconds())
			}()
		}
	}

	onError := func(p *player.Player, err error) {
		fmt.Printf("Got err %v\n", err)
		p.Next()
	}

	config := &player.Config{
		Repeat:  options.Repeat,
		OnTrack: onTrack,
		OnPause: onPause,
		OnError: onError,
	}
	player := player.NewPlayer(playout, playlist, config)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		player.Stop()
	}()

	view.OnStart(player)
	player.Start()
	view.OnStop()

	return nil
}
