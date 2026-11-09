# Turn Den Light on when the FP300 detects presence and off when it clears.
# The sensor's own occupancy hold time determines when the off event arrives.
den = device("Den Light", endpoint=1)
presence = device("Den Presence Sensor")

on(occupied(presence, endpoint=1), lambda event: [power(den, "on")])
on(unoccupied(presence, endpoint=1), lambda event: [power(den, "off")])
