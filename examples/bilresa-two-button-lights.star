# Bind one BILRESA two-button switch to all lights in a room. Set the switch
# endpoint IDs from its inventory; they are not the scroll wheel's endpoints.

def bind_light(button, on_endpoint, off_endpoint, light):
    on(short_release(button, endpoint=on_endpoint),
       lambda event: [power(light, "on")])
    on(short_release(button, endpoint=off_endpoint),
       lambda event: [power(light, "off")])
    on(long_press(button, endpoint=on_endpoint),
       lambda event: [
           power(light, "on"),
           brightness(light, 100),
           colour_temperature(light, 4000),
       ])
    on(long_press(button, endpoint=off_endpoint),
       lambda event: [
           power(light, "on"),
           brightness(light, 1),
           warmest_colour_temperature(light),
       ])

def bind_room(button_selector, on_endpoint, off_endpoint, lights):
    button = device(button_selector)
    for light_selector, light_endpoint in lights:
        bind_light(button, on_endpoint, off_endpoint,
                   device(light_selector, endpoint=light_endpoint))

# Replace these selectors and endpoint IDs with the first room's devices.
bind_room("ROOM_1_BILRESA", 1, 2, [("ROOM_1_LIGHT", 1)])

# Add further rooms with another call:
# bind_room("ROOM_2_BILRESA", 1, 2, [("CEILING_LIGHT", 1), ("TABLE_LIGHT", 1)])
