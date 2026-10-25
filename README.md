# trainspot

Live NMBS train departures for any station in Belgium, right in your terminal.

```sh
go install github.com/PieterLuypaert/trainspot@latest
```

```sh
trainspot gent sint pieters               # the departure board of Gent-Sint-Pieters
trainspot mons                            # Bergen and Mons are the same station
trainspot liège-guillemins --lang fr      # station names in French: nl, fr, en or de
```

For every departure the board shows when it is planned and when it really leaves:
on time, the new time with the minutes late, cancelled or departed. Next to that come
the destination, the platform (black on yellow when it changed), the train and how
busy NMBS expects it to be. A legend explains the marks on the board, and on a narrow
terminal the least important columns make way.

Station names work in Dutch, French, English and German, with or without accents.
When a name fits several stations, such as `gent` or `brussel`, trainspot lists them
and asks which one you mean. Typos get suggestions.

The departures come live from the [iRail API](https://docs.irail.be), which serves the
NMBS's own data. trainspot keeps the list of stations for a week in `~/.cache/trainspot`.
