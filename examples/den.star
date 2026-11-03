# Example for the current Den Light, BILRESA scroll wheel, and FP300 sensor.
# Device labels are exact Matter labels; use a node ID where no label exists.
den = device("Den Light", endpoint=1)
bilresa = device("2")
presence = device("Presence Multi-Sensor FP300")

def bind_toggle(button, endpoint, light):
    on(short_press(button, endpoint=endpoint), lambda event: [toggle(light)])

def daytime(event):
    return [
        power(den, "on"),
        brightness(den, 90),
        colour_temperature(den, 5000),
    ]

on(occupied(presence, endpoint=1), daytime)
on(wheel(bilresa, up=7, down=8),
   lambda event: [adjust_brightness(den, event.steps * 5)])
bind_toggle(bilresa, 9, den)
