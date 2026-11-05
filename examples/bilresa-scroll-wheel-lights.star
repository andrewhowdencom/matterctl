# Bind one BILRESA scroll wheel to a room of lights. The three LED groups
# select which property the wheel changes. A single click toggles each light,
# a double click turns them all on, and a long press turns them all off.
# Verify the switch's endpoint IDs with event watch.

def bind_button(button, endpoint, light):
    on(single_press(button, endpoint=endpoint),
       lambda event: [toggle(light)])
    on(double_press(button, endpoint=endpoint),
       lambda event: [power(light, "on")])
    on(long_press(button, endpoint=endpoint),
       lambda event: [power(light, "off")])

def bind_light(button, light, colour, temperature, brightness):
    bind_button(button, colour[2], light)
    bind_button(button, temperature[2], light)
    bind_button(button, brightness[2], light)
    on(wheel_step(button, up=colour[0], down=colour[1]),
       lambda event: [adjust_colour(light, event.steps * 12)])
    on(wheel_step(button, up=temperature[0], down=temperature[1]),
       lambda event: [adjust_colour_temperature(light, event.steps * 150)])
    on(wheel_step(button, up=brightness[0], down=brightness[1]),
       lambda event: [adjust_brightness(light, event.steps * 5)])

def bind_room(button_selector, lights, colour=(1, 2, 3),
              temperature=(4, 5, 6), brightness=(7, 8, 9)):
    button = device(button_selector)
    for light_selector, light_endpoint in lights:
        bind_light(button, device(light_selector, endpoint=light_endpoint),
                   colour, temperature, brightness)

# Group 3 keeps the existing brightness behavior for Den Light.
bind_room("Den Scroll Wheel", [("Den Light", 1)])

# Add rooms, each with its own BILRESA and any number of lights:
# bind_room("ROOM_2_BILRESA", [("CEILING_LIGHT", 1), ("TABLE_LIGHT", 1)])
