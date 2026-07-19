import FreeCAD as App
import Part, math

doc = App.getDocument('FlangeCoupling')
print('Got doc:', doc.Name)

# Find current model
objs = doc.Objects
for o in objs:
    print(f'  {o.Name} ({o.TypeId})')

# Get the last good object (FlangeKeyed)
flange_base = None
for o in objs:
    if o.Name == 'FlangeKeyed':
        flange_base = o
        break

if flange_base is None:
    # Try FlangeHoles
    for o in objs:
        if o.Name == 'FlangeHoles':
            flange_base = o
            break

print(f'Base object: {flange_base.Name if flange_base else None}')
