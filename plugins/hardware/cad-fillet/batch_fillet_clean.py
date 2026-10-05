import FreeCAD as App
doc = App.getDocument('FlangeCoupling')

# Remove test fillet
doc.removeObject('TestFillet')
doc.recompute()

obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges

# Select edges with length > 10mm
sel = []
for i, e in enumerate(edges):
    if e.Length > 10:
        sel.append((i+1, 2.0, 2.0))

print(f'Selected {len(sel)} edges with length > 10mm')

# Try in batches of 20
batch_size = 20
current_obj = obj
for batch_start in range(0, len(sel), batch_size):
    batch = sel[batch_start:batch_start+batch_size]
    name = f'Fillet_{batch_start//batch_size}'
    fillet = doc.addObject('Part::Fillet', name)
    fillet.Base = current_obj
    fillet.Edges = batch
    doc.recompute()
    try:
        if fillet.Shape.isValid():
            current_obj = fillet
            print(f'  Batch {batch_start//batch_size}: OK ({len(batch)} edges)')
        else:
            print(f'  Batch {batch_start//batch_size}: INVALID, skipping')
            doc.removeObject(name)
    except Exception as e:
        print(f'  Batch {batch_start//batch_size}: ERROR {e}')
        doc.removeObject(name)
        break

print(f'Final object: {current_obj.Name}')
try:
    vol = current_obj.Shape.Volume
    print(f'Final Volume: {vol:.1f} mm3')
except:
    print('Could not compute volume')

current_obj.Label = 'FlangeCoupling_Filleted'
