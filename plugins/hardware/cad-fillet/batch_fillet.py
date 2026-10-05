import FreeCAD as App
doc = App.getDocument('FlangeCoupling')

# Clean up any old fillets
to_remove = [o for o in doc.Objects if 'Fillet' in o.Name]
for o in to_remove:
    try:
        doc.removeObject(o.Name)
    except:
        pass
doc.recompute()

obj = doc.getObject('FlangeRibbed')
edges = obj.Shape.Edges

# Select edges with length > 10mm
sel = []
for i, e in enumerate(edges):
    if e.Length > 10:
        sel.append((i+1, 2.0, 2.0))

print(f'Total candidates: {len(sel)}')

# Try in batches of 5
batch_size = 5
current_obj = obj
ok_count = 0
for batch_start in range(0, min(len(sel), 30), batch_size):
    batch = sel[batch_start:batch_start+batch_size]
    name = f'F_{batch_start//batch_size}'
    fillet = doc.addObject('Part::Fillet', name)
    fillet.Base = current_obj
    fillet.Edges = batch
    doc.recompute()
    try:
        if fillet.Shape and fillet.Shape.isValid():
            current_obj = fillet
            ok_count += len(batch)
            print(f'  Batch {batch_start//batch_size}: OK')
        else:
            print(f'  Batch {batch_start//batch_size}: INVALID')
            doc.removeObject(name)
    except Exception as e:
        print(f'  Batch {batch_start//batch_size}: ERROR')
        doc.removeObject(name)

print(f'Filleted {ok_count} edges')
current_obj.Label = 'FlangeFinal'
try:
    vol = current_obj.Shape.Volume
    print(f'Volume: {vol:.1f} mm3')
except Exception as e:
    print(f'Vol err: {e}')
