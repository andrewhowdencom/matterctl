# Bind one BILRESA two-button switch to one light per room. Selectors may be
# exact Matter labels or node IDs. Set the two button endpoint IDs from the
# switch's inventory; they are not the scroll wheel BILRESA's endpoint IDs.
def bind_room(button_selector, on_endpoint, off_endpoint, light_selector, light_endpoint):
    button = device(button_selector)
    light = device(light_selector, endpoint=light_endpoint)

    on(short_release(button, endpoint=on_endpoint),
       lambda event: [power(light, "on")])
    on(short_release(button, endpoint=off_endpoint),
       lambda event: [power(light, "off")])
    on(long_press(button, endpoint=on_endpoint),
       lambda event: [
           power(light, "on"),
           brightness(light, 100),
           colour_temperature(light, 5000),
       ])
    on(long_press(button, endpoint=off_endpoint),
       lambda event: [
           power(light, "on"),
           brightness(light, 1),
           warmest_colour_temperature(light),
       ])

# Replace these selectors and endpoint IDs with the first room's devices.
bind_room("ROOM_1_BILRESA", 1, 2, "ROOM_1_LIGHT", 1)

# Add further rooms with another call, using their own device IDs or labels:
# bind_room("ROOM_2_BILRESA", 1, 2, "ROOM_2_LIGHT", 1)
